/*
 * psnr.c - see psnr.h. The socket is nonblocking after connect; psnr_poll
 * reads whatever has arrived, cuts complete frames into a FIFO, and hands
 * them out one at a time.
 */
#include "psnr.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

#ifdef _WIN32
#  if !defined(_WIN32_WINNT) || _WIN32_WINNT < 0x0600
#    undef _WIN32_WINNT
#    define _WIN32_WINNT 0x0600   /* WSAPoll, GetTickCount64 (MinGW defaults older) */
#  endif
#  define WIN32_LEAN_AND_MEAN
#  include <winsock2.h>
#  include <ws2tcpip.h>
#  ifdef _MSC_VER
#    pragma comment(lib, "ws2_32.lib")
#  endif
   typedef SOCKET sock_t;
#  define BAD_SOCK INVALID_SOCKET
#  define sock_close closesocket
#  define WOULD_BLOCK() (WSAGetLastError() == WSAEWOULDBLOCK)
#  define poll WSAPoll
   typedef WSAPOLLFD pollfd_t;
#else
#  include <sys/socket.h>
#  include <netdb.h>
#  include <netinet/in.h>
#  include <netinet/tcp.h>
#  include <fcntl.h>
#  include <unistd.h>
#  include <errno.h>
#  include <poll.h>
   typedef int sock_t;
#  define BAD_SOCK (-1)
#  define sock_close close
#  define WOULD_BLOCK() (errno == EAGAIN || errno == EWOULDBLOCK)
   typedef struct pollfd pollfd_t;
#endif

#define HEARTBEAT_SECS 20

static uint64_t now_ms(void)
{
#ifdef _WIN32
    return GetTickCount64();
#else
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (uint64_t)ts.tv_sec * 1000u + (uint64_t)ts.tv_nsec / 1000000u;
#endif
}

typedef struct node { psnr_msg m; struct node* next; } node;

struct psnr_client {
    sock_t   s;
    uint32_t next_req;
    uint8_t* rx;          /* bytes received, not yet framed */
    size_t   rx_len, rx_cap;
    node    *head, *tail; /* framed messages waiting for psnr_poll */
    time_t   last_send;
    int      dead;
};

static int send_all(psnr_client* c, const uint8_t* p, size_t n)
{
    while (n) {
        int k = send(c->s, (const char*)p, (int)n, 0);
        if (k < 0) {
            pollfd_t pf;
            if (!WOULD_BLOCK()) { c->dead = 1; return -1; }
            pf.fd = c->s; pf.events = POLLOUT; pf.revents = 0;
            poll(&pf, 1, 1000);
            continue;
        }
        p += k;
        n -= (size_t)k;
    }
    c->last_send = time(NULL);
    return 0;
}

uint32_t psnr_send(psnr_client* c, uint8_t type, const void* body, uint32_t len)
{
    uint8_t hdr[7];
    if (c->dead || len > 0xFFFF - 4) return 0;
    if (++c->next_req == 0) c->next_req = 1;   /* 0 means "push" */
    hdr[0] = type;
    psnr_put16(hdr + 1, (uint16_t)(len + 4));
    psnr_put32(hdr + 3, c->next_req);
    if (send_all(c, hdr, 7) || (len && send_all(c, (const uint8_t*)body, len))) return 0;
    return c->next_req;
}

/* Pull what the socket has and frame it. -1 once the connection is gone. */
static int pump(psnr_client* c)
{
    for (;;) {
        if (c->rx_cap - c->rx_len < 4096) {
            c->rx_cap = c->rx_cap ? c->rx_cap * 2 : 8192;
            c->rx = (uint8_t*)realloc(c->rx, c->rx_cap);
        }
        int k = recv(c->s, (char*)c->rx + c->rx_len, (int)(c->rx_cap - c->rx_len), 0);
        if (k == 0 || (k < 0 && !WOULD_BLOCK())) { c->dead = 1; break; }
        if (k < 0) break;
        c->rx_len += (size_t)k;
    }

    size_t off = 0;
    while (c->rx_len - off >= 3) {
        uint8_t  type = c->rx[off];
        uint16_t n = psnr_get16(c->rx + off + 1);
        if (c->rx_len - off < 3u + n) break;
        const uint8_t* p = c->rx + off + 3;

        /* Replies (0x8x, 0x9x, 0xBx) start with the request id; pushes (0xAx) don't. */
        int has_req = (type & 0xF0) != 0xA0;
        node* nd = (node*)calloc(1, sizeof(node));
        if (has_req && n >= 4) { nd->m.req = psnr_get32(p); p += 4; n -= 4; }
        nd->m.type = type;
        nd->m.len = n;
        nd->m.data = (uint8_t*)malloc(n ? n : 1);
        memcpy(nd->m.data, p, n);
        if (c->tail) c->tail->next = nd; else c->head = nd;
        c->tail = nd;
        off += 3u + psnr_get16(c->rx + off + 1);
    }
    memmove(c->rx, c->rx + off, c->rx_len - off);
    c->rx_len -= off;
    return c->dead ? -1 : 0;
}

static int take(psnr_client* c, psnr_msg* out, uint32_t want_req)
{
    node *prev = NULL, *nd = c->head;
    for (; nd; prev = nd, nd = nd->next)
        if (!want_req || nd->m.req == want_req) break;
    if (!nd) return 0;
    if (prev) prev->next = nd->next; else c->head = nd->next;
    if (c->tail == nd) c->tail = prev;
    *out = nd->m;
    free(nd);
    return 1;
}

int psnr_poll(psnr_client* c, psnr_msg* out)
{
    if (!c->dead && time(NULL) - c->last_send >= HEARTBEAT_SECS)
        psnr_send(c, PSNR_HEARTBEAT, NULL, 0);
    if (!c->dead) pump(c);
    if (take(c, out, 0)) return 1;
    return c->dead ? -1 : 0;
}

int psnr_call(psnr_client* c, uint8_t type, const void* body, uint32_t len,
              psnr_msg* reply, int timeout_ms)
{
    uint32_t req = psnr_send(c, type, body, len);
    uint64_t end = now_ms() + (uint64_t)timeout_ms;
    if (!req) return -1;
    for (;;) {
        pollfd_t pf;
        if (!c->dead) pump(c);
        if (take(c, reply, req)) return 1;
        if (c->dead) return -1;
        if (now_ms() >= end) return 0;
        pf.fd = c->s; pf.events = POLLIN; pf.revents = 0;
        poll(&pf, 1, 10);
    }
}

void psnr_msg_free(psnr_msg* m)
{
    free(m->data);
    m->data = NULL;
}

static int s_connect_error;

psnr_client* psnr_connect(const char* host, uint16_t port,
                          const char* comm_id, const char* online_id, uint16_t p2p_port,
                          uint32_t* out_user_id, uint8_t out_public_ip[4])
{
    struct addrinfo hints, *res = NULL, *ai;
    char portstr[8];
    sock_t s = BAD_SOCK;

#ifdef _WIN32
    WSADATA w;
    WSAStartup(MAKEWORD(2, 2), &w);
#endif
    memset(&hints, 0, sizeof(hints));
    hints.ai_family = AF_INET;
    hints.ai_socktype = SOCK_STREAM;
    snprintf(portstr, sizeof(portstr), "%u", port);
    if (getaddrinfo(host, portstr, &hints, &res) != 0) return NULL;
    for (ai = res; ai; ai = ai->ai_next) {
        s = socket(ai->ai_family, ai->ai_socktype, ai->ai_protocol);
        if (s == BAD_SOCK) continue;
        if (connect(s, ai->ai_addr, (int)ai->ai_addrlen) == 0) break;
        sock_close(s);
        s = BAD_SOCK;
    }
    freeaddrinfo(res);
    if (s == BAD_SOCK) return NULL;

    {   /* small request/reply frames: don't let Nagle sit on them */
        int one = 1;
        setsockopt(s, IPPROTO_TCP, TCP_NODELAY, (const char*)&one, sizeof(one));
    }
#ifdef _WIN32
    { u_long nb = 1; ioctlsocket(s, FIONBIO, &nb); }
#else
    fcntl(s, F_SETFL, fcntl(s, F_GETFL, 0) | O_NONBLOCK);
#endif

    psnr_client* c = (psnr_client*)calloc(1, sizeof(psnr_client));
    c->s = s;

    /* HELLO: comm_id [12] | online_id [16] | p2p_port u16 */
    uint8_t hello[30] = {0};
    strncpy((char*)hello, comm_id, 12);
    strncpy((char*)hello + 12, online_id, 16);
    psnr_put16(hello + 28, p2p_port);
    psnr_msg ack;
    s_connect_error = 0;
    if (psnr_call(c, PSNR_HELLO, hello, sizeof(hello), &ack, 5000) != 1) {
        psnr_close(c);
        return NULL;
    }
    int ok = ack.type == PSNR_HELLO_ACK && ack.len >= 8;
    s_connect_error = (ack.type == PSNR_ERROR && ack.len >= 4) ? (int)psnr_get32(ack.data) : 0;
    if (ok && out_user_id) *out_user_id = psnr_get32(ack.data);
    if (ok && out_public_ip) memcpy(out_public_ip, ack.data + 4, 4);
    psnr_msg_free(&ack);
    if (!ok) { psnr_close(c); return NULL; }
    return c;
}

/* ponytail: one static, not per connection -- a process connects once. */
int psnr_connect_error(void)
{
    return s_connect_error;
}

void psnr_close(psnr_client* c)
{
    psnr_msg m;
    if (!c) return;
    sock_close(c->s);
    c->dead = 1;
    while (take(c, &m, 0)) psnr_msg_free(&m);
    free(c->rx);
    free(c);
}

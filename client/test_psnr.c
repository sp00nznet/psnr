/*
 * test_psnr - the C client against a live server: HELLO, a room joined by a
 * second client, the push that tells the host, a room message, a score.
 * The server's Go tests build and run this when a C compiler is on PATH:
 *
 *   test_psnr <host> <port>
 */
#include "psnr.h"

#include <assert.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#ifdef _WIN32
#  include <winsock2.h>
#  include <windows.h>
#  define sleep_ms(n) Sleep(n)
   typedef int socklen_t;
#else
#  include <unistd.h>
#  include <sys/socket.h>
#  include <netinet/in.h>
#  include <fcntl.h>
#  define sleep_ms(n) usleep((n) * 1000)
#endif

#define COMM "NPWR00001_00"

/* A UDP socket, as a title's P2P socket would be, probes the server and gets
 * back the address the server saw it at. */
static void probe(psnr_client* c)
{
    int u = (int)socket(AF_INET, SOCK_DGRAM, 0);
    struct sockaddr_in me, srv;
    socklen_t n = sizeof(me);
    uint8_t pkt[PSNR_UDP_PROBE_LEN], reply[64], ip[4];
    uint16_t port;
    assert(u >= 0);
    memset(&me, 0, sizeof(me));
    me.sin_family = AF_INET;
    assert(bind(u, (struct sockaddr*)&me, sizeof(me)) == 0);
    getsockname(u, (struct sockaddr*)&me, &n);
#ifdef _WIN32
    { u_long nb = 1; ioctlsocket(u, FIONBIO, &nb); }
#else
    fcntl(u, F_SETFL, O_NONBLOCK);
#endif
    memset(&srv, 0, sizeof(srv));
    srv.sin_family = AF_INET;
    psnr_server_addr(c, (uint8_t*)&srv.sin_addr, &port);
    srv.sin_port = htons(port);
    psnr_probe_packet(c, pkt);
    assert(psnr_is_control(pkt, sizeof(pkt)));
    for (int i = 0; i < 200; i++) {   /* ~2 s, one probe per 100 ms */
        if (i % 20 == 0) sendto(u, (const char*)pkt, sizeof(pkt), 0, (struct sockaddr*)&srv, sizeof(srv));
        int r = (int)recv(u, (char*)reply, sizeof(reply), 0);
        if (r > 0 && psnr_probe_reply(reply, (size_t)r, ip, &port)) {
            assert(port == ntohs(me.sin_port) && ip[0] == 127);
#ifdef _WIN32
            closesocket(u);
#else
            close(u);
#endif
            return;
        }
        sleep_ms(10);
    }
    fprintf(stderr, "no probe reply\n");
    exit(1);
}

static psnr_msg wait_push(psnr_client* c, uint8_t type)
{
    psnr_msg m;
    for (int i = 0; i < 400; i++) {   /* ~2 s */
        int r = psnr_poll(c, &m);
        assert(r >= 0);
        if (r == 1) {
            assert(m.req == 0 && m.type == type);
            return m;
        }
        sleep_ms(5);
    }
    fprintf(stderr, "no push 0x%02X\n", type);
    exit(1);
}

int main(int argc, char** argv)
{
    const char* host = argc > 1 ? argv[1] : "127.0.0.1";
    uint16_t port = (uint16_t)(argc > 2 ? atoi(argv[2]) : 36100);
    uint32_t ida, idb;
    uint8_t ip[4];

    psnr_client* a = psnr_connect(host, port, COMM, "alice", 3658, &ida, ip);
    psnr_client* b = psnr_connect(host, port, COMM, "bob", 3659, &idb, ip);
    assert(a && b && ida != idb);
    assert(ip[0] != 0);   /* the address the server sees us at */
    probe(a);
    {   /* a punch is a control packet; a title's own data isn't */
        uint8_t punch[PSNR_UDP_PUNCH_LEN];
        psnr_punch_packet(a, punch);
        assert(psnr_is_control(punch, sizeof(punch)) && !psnr_is_control("hello", 5));
    }

    /* alice hosts: max 4 | flags 0 | external "x" | internal "" | member data "" */
    uint8_t body[64], *p = body;
    psnr_msg m;
    *p++ = 4;
    p = psnr_put32(p, 0);
    p = psnr_put16(p, 1);
    *p++ = 'x';
    p = psnr_put16(p, 0);
    p = psnr_put16(p, 0);
    assert(psnr_call(a, PSNR_CREATE_ROOM, body, (uint32_t)(p - body), &m, 2000) == 1);
    assert(m.type == PSNR_ROOM_JOINED);
    uint64_t room = psnr_get64(m.data);
    psnr_msg_free(&m);

    /* bob searches and joins */
    p = psnr_put16(body, 0);
    p = psnr_put16(p, 10);
    assert(psnr_call(b, PSNR_SEARCH_ROOMS, body, 4, &m, 2000) == 1);
    assert(m.type == PSNR_ROOM_LIST && psnr_get16(m.data + 2) == 1 && psnr_get64(m.data + 4) == room);
    psnr_msg_free(&m);
    p = psnr_put64(body, room);
    p = psnr_put16(p, 0);   /* no member data */
    assert(psnr_call(b, PSNR_JOIN_ROOM, body, (uint32_t)(p - body), &m, 2000) == 1);
    assert(m.type == PSNR_ROOM_JOINED);
    psnr_msg_free(&m);

    /* alice makes an unrelated call first: the MEMBER_JOINED push that lands
     * meanwhile must stay queued for psnr_poll, not be eaten by psnr_call. */
    p = psnr_put32(body, 1);
    *p++ = 0;
    p = psnr_put64(p, 100);
    p = psnr_put16(p, 0);
    assert(psnr_call(a, PSNR_RECORD_SCORE, body, (uint32_t)(p - body), &m, 2000) == 1);
    assert(m.type == PSNR_SCORE_RECORDED && psnr_get32(m.data) == 1);
    psnr_msg_free(&m);

    m = wait_push(a, PSNR_MEMBER_JOINED);
    /* room u64 | member u16 | user u32 | online_id [16] | ip [4] | port u16 | owner u8 */
    assert(psnr_get64(m.data) == room);
    assert(psnr_get32(m.data + 10) == idb);
    assert(strcmp((const char*)m.data + 14, "bob") == 0);
    assert(psnr_get16(m.data + 34) == 3659);
    psnr_msg_free(&m);

    /* bob -> everyone */
    p = psnr_put64(body, room);
    p = psnr_put16(p, 0);
    p = psnr_put16(p, 2);
    *p++ = 'h';
    *p++ = 'i';
    assert(psnr_call(b, PSNR_ROOM_MESSAGE, body, (uint32_t)(p - body), &m, 2000) == 1);
    assert(m.type == PSNR_OK);
    psnr_msg_free(&m);
    m = wait_push(a, PSNR_ROOM_MSG);
    /* room u64 | from u16 | to u16 | blob */
    assert(psnr_get16(m.data + 8) == 2 && psnr_get16(m.data + 10) == 0);
    assert(psnr_get16(m.data + 12) == 2 && memcmp(m.data + 14, "hi", 2) == 0);
    psnr_msg_free(&m);

    psnr_close(b);
    m = wait_push(a, PSNR_MEMBER_LEFT);
    psnr_msg_free(&m);
    psnr_close(a);

    printf("test_psnr: all passed\n");
    return 0;
}

/*
 * natcheck - one player in the NAT lab (lab/README.md). It does what the
 * runtime does for a title, without the title:
 *   1. HELLO, then probe the server from its P2P socket, so the server learns
 *      the socket's public endpoint;
 *   2. host or join a room, which hands it the other player's endpoint;
 *   3. punch and ping that endpoint until a PONG comes back, switching to the
 *      server's relay if nothing gets through directly;
 *   4. a stream between them, the way a title sends its game setup: the host
 *      connects to the joiner, directly or through the relay as the server's
 *      ROUTE push says.
 * Exits 0 once both the ping and the stream worked.
 *
 *   natcheck <server> <name> host|join [p2p_port]
 *
 * POSIX only: the lab runs it in Linux containers.
 */
#include "../client/psnr.h"

#include <arpa/inet.h>
#include <fcntl.h>
#include <netinet/in.h>
#include <poll.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <time.h>
#include <unistd.h>

#define COMM "NPWR00001_00"

static const char* g_name;

static void die(const char* why)
{
    printf("FAIL %s: %s\n", g_name, why);
    exit(1);
}

static double now(void)
{
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return ts.tv_sec + ts.tv_nsec / 1e9;
}

static psnr_msg call(psnr_client* c, uint8_t type, const void* body, uint32_t len)
{
    psnr_msg m;
    if (psnr_call(c, type, body, len, &m, 3000) != 1) die("no reply from the server");
    return m;
}

/* Member entry: member u16 | user u32 | online_id [16] | ip [4] | port u16 |
 * owner u8 | data blob. Returns the bytes it took. */
static uint32_t read_member(const uint8_t* p, uint32_t* user, struct sockaddr_in* at)
{
    *user = psnr_get32(p + 2);
    memset(at, 0, sizeof(*at));
    at->sin_family = AF_INET;
    memcpy(&at->sin_addr, p + 22, 4);
    at->sin_port = htons(psnr_get16(p + 26));
    return 29 + 2 + psnr_get16(p + 29);
}

/* Pushes that matter here, collected whenever the connection is polled. */
static uint32_t g_peer_user;
static struct sockaddr_in g_peer;
static int g_relay_stream;          /* ROUTE: streams to the peer go through the relay */
static uint32_t g_offer_id;         /* STREAM_OFFER waiting to be accepted */

static void take_push(const psnr_msg* m)
{
    if (m->type == PSNR_MEMBER_JOINED && m->len > 8)
        read_member(m->data + 8, &g_peer_user, &g_peer);
    else if (m->type == PSNR_ROUTE && m->len >= 13 && psnr_get32(m->data + 8) != 0)
        g_relay_stream = (m->data[12] & PSNR_ROUTE_STREAM_RELAY) != 0;
    else if (m->type == PSNR_STREAM_OFFER && m->len >= 10)
        g_offer_id = psnr_get32(m->data);
}

static void poll_server(psnr_client* c)
{
    psnr_msg m;
    int r;
    while ((r = psnr_poll(c, &m)) == 1) {
        take_push(&m);
        psnr_msg_free(&m);
    }
    if (r < 0) die("lost the server");
}

/* A line over a stream: write ours, read theirs. */
static void stream_talk(int s, const char* mine, char* theirs, size_t cap)
{
    struct pollfd p = { .fd = s, .events = POLLIN };
    if (send(s, mine, strlen(mine), 0) != (ssize_t)strlen(mine)) die("stream send failed");
    if (poll(&p, 1, 5000) != 1) die("nothing came over the stream");
    ssize_t n = recv(s, theirs, cap - 1, 0);
    if (n <= 0) die("the stream closed");
    theirs[n] = 0;
}

int main(int argc, char** argv)
{
    if (argc < 4) {
        fprintf(stderr, "usage: natcheck <server> <name> host|join [p2p_port]\n");
        return 2;
    }
    const char* server = argv[1];
    g_name = argv[2];
    int host = strcmp(argv[3], "host") == 0;
    uint16_t p2p = (uint16_t)(argc > 4 ? atoi(argv[4]) : 3658);
    setvbuf(stdout, NULL, _IONBF, 0);

    uint32_t me = 0;
    uint8_t seen[4];
    psnr_client* c = NULL;
    for (int i = 0; i < 50 && !c; i++) {   /* the server may still be starting */
        c = psnr_connect(server, 36100, COMM, g_name, p2p, &me, seen);
        if (!c) usleep(200 * 1000);
    }
    if (!c) die("can't reach the server");

    /* The P2P socket, and (joiner) the P2P stream listener on the same port. */
    int u = socket(AF_INET, SOCK_DGRAM, 0);
    struct sockaddr_in local = { .sin_family = AF_INET, .sin_port = htons(p2p) };
    if (bind(u, (struct sockaddr*)&local, sizeof(local)) != 0) die("can't bind the P2P port");
    fcntl(u, F_SETFL, O_NONBLOCK);
    int lst = -1;
    if (!host) {
        int one = 1;
        lst = socket(AF_INET, SOCK_STREAM, 0);
        setsockopt(lst, SOL_SOCKET, SO_REUSEADDR, &one, sizeof(one));
        if (bind(lst, (struct sockaddr*)&local, sizeof(local)) != 0 || listen(lst, 4) != 0)
            die("can't listen on the P2P port");
    }

    /* 1. Probe until the server says where it sees the P2P socket. */
    struct sockaddr_in srv = { .sin_family = AF_INET };
    uint16_t sport, pubport = 0;
    uint8_t pkt[256], ip[4];
    psnr_server_addr(c, (uint8_t*)&srv.sin_addr, &sport);
    srv.sin_port = htons(sport);
    for (double t0 = now(); !pubport; usleep(20 * 1000)) {
        if (now() - t0 > 5) die("no probe reply");
        psnr_probe_packet(c, pkt);
        sendto(u, pkt, PSNR_UDP_PROBE_LEN, 0, (struct sockaddr*)&srv, sizeof(srv));
        ssize_t n = recv(u, pkt, sizeof(pkt), 0);
        if (n > 0 && psnr_probe_reply(pkt, (size_t)n, ip, &pubport)) break;
    }
    printf("%s: P2P socket seen at %u.%u.%u.%u:%u\n", g_name, ip[0], ip[1], ip[2], ip[3], pubport);

    /* 2. Meet the other player through a room. */
    uint8_t body[64], *p;
    psnr_msg m;
    if (host) {
        p = body;
        *p++ = 2;
        p = psnr_put32(p, 0);
        p = psnr_put16(p, 0);
        p = psnr_put16(p, 0);
        p = psnr_put16(p, 0);
        m = call(c, PSNR_CREATE_ROOM, body, (uint32_t)(p - body));
        if (m.type != PSNR_ROOM_JOINED) die("create room failed");
        psnr_msg_free(&m);
        for (double t0 = now(); !g_peer_user; usleep(20 * 1000)) {
            if (now() - t0 > 30) die("nobody joined");
            poll_server(c);
        }
    } else {
        uint64_t room = 0;
        for (double t0 = now(); !room; usleep(200 * 1000)) {
            if (now() - t0 > 30) die("no room to join");
            p = psnr_put16(body, 0);
            p = psnr_put16(p, 10);
            m = call(c, PSNR_SEARCH_ROOMS, body, 4);
            if (m.type == PSNR_ROOM_LIST && psnr_get16(m.data + 2) > 0) room = psnr_get64(m.data + 4);
            psnr_msg_free(&m);
        }
        p = psnr_put64(body, room);
        p = psnr_put16(p, 0);
        m = call(c, PSNR_JOIN_ROOM, body, (uint32_t)(p - body));
        if (m.type != PSNR_ROOM_JOINED) die("join failed");
        const uint8_t* q = m.data + 17;
        q += 2 + psnr_get16(q);
        q += 2 + psnr_get16(q);
        for (int i = 0, n = *q++; i < n; i++) {
            uint32_t user;
            struct sockaddr_in at;
            q += read_member(q, &user, &at);
            if (user != me) { g_peer_user = user; g_peer = at; }
        }
        psnr_msg_free(&m);
        if (!g_peer_user) die("the room has nobody else");
    }
    usleep(300 * 1000);
    poll_server(c);   /* the ROUTE push about the peer */
    printf("%s: the other player is at %s:%u%s\n", g_name, inet_ntoa(g_peer.sin_addr),
           ntohs(g_peer.sin_port), g_relay_stream ? " (streams via the relay)" : "");

    /* 3. Punch and ping until a PONG comes back. Nothing direct from the
     * peer within 3 s means a router won't let it through: use the relay. */
    uint8_t punch[PSNR_UDP_PUNCH_LEN];
    char ping[40], pong[40];
    int relay_udp = 0;
    psnr_punch_packet(c, punch);
    snprintf(ping, sizeof(ping), "PING %s", g_name);
    snprintf(pong, sizeof(pong), "PONG %s", g_name);
    double done = 0, last = 0, heard = 0, t0 = now();
    for (; !done || now() - done < 2; usleep(10 * 1000)) {
        if (!done && now() - t0 > 12) die("no PONG: the other player never got through");
        if (!heard && !relay_udp && now() - t0 > 3 && psnr_relay_available(c)) {
            relay_udp = 1;
            printf("%s: nothing direct from the other player; using the relay\n", g_name);
        }
        if (now() - last > 0.2) {
            if (relay_udp) {
                size_t n = psnr_relay_wrap(c, g_peer_user, ping, strlen(ping), pkt, sizeof(pkt));
                sendto(u, pkt, n, 0, (struct sockaddr*)&srv, sizeof(srv));
            } else {
                sendto(u, punch, sizeof(punch), 0, (struct sockaddr*)&g_peer, sizeof(g_peer));
                sendto(u, ping, strlen(ping), 0, (struct sockaddr*)&g_peer, sizeof(g_peer));
            }
            last = now();
        }
        struct sockaddr_in from;
        socklen_t fl = sizeof(from);
        ssize_t n = recvfrom(u, pkt, sizeof(pkt) - 1, 0, (struct sockaddr*)&from, &fl);
        if (n <= 0) { poll_server(c); continue; }
        uint32_t via;
        const uint8_t* payload = pkt;
        size_t plen = (size_t)n;
        int relayed = psnr_relay_unwrap(pkt, (size_t)n, &via, &payload, &plen);
        if (!relayed) {
            if (psnr_is_control(pkt, (size_t)n)) {   /* a punch: the direct path works */
                if (from.sin_addr.s_addr == g_peer.sin_addr.s_addr) heard = now();
                continue;
            }
            heard = now();
        }
        char msg[64];
        memcpy(msg, payload, plen < sizeof(msg) ? plen : sizeof(msg) - 1);
        msg[plen < sizeof(msg) ? plen : sizeof(msg) - 1] = 0;
        if (!strncmp(msg, "PING", 4)) {
            if (relayed) {
                size_t w = psnr_relay_wrap(c, via, pong, strlen(pong), pkt, sizeof(pkt));
                sendto(u, pkt, w, 0, (struct sockaddr*)&srv, sizeof(srv));
            } else {
                sendto(u, pong, strlen(pong), 0, (struct sockaddr*)&from, fl);
            }
        } else if (!strncmp(msg, "PONG", 4) && !done) {
            printf("PASS %s: \"%s\" %s\n", g_name, msg, relayed ? "through the relay" : "direct");
            done = now();
        }
    }

    /* 4. A stream, host to joiner, like a title's game setup. */
    char got[64];
    if (host) {
        int s;
        if (g_relay_stream) {
            s = (int)psnr_stream_connect(c, g_peer_user, ntohs(g_peer.sin_port), 12000);
            if (s < 0) die("the relay didn't give a stream");
        } else {
            s = socket(AF_INET, SOCK_STREAM, 0);
            if (connect(s, (struct sockaddr*)&g_peer, sizeof(g_peer)) != 0) die("stream connect failed");
        }
        char setup[40];
        snprintf(setup, sizeof(setup), "SETUP from %s", g_name);
        stream_talk(s, setup, got, sizeof(got));
        printf("PASS %s: stream %s, got \"%s\"\n", g_name, g_relay_stream ? "through the relay" : "direct", got);
        close(s);
    } else {
        int s = -1, via_relay = 0;
        for (double t1 = now(); s < 0; usleep(20 * 1000)) {
            if (now() - t1 > 20) die("no stream from the host");
            struct pollfd pl = { .fd = lst, .events = POLLIN };
            if (poll(&pl, 1, 0) == 1) s = accept(lst, NULL, NULL);
            poll_server(c);
            if (s < 0 && g_offer_id) {
                s = (int)psnr_stream_accept(c, g_offer_id, 5000);
                via_relay = 1;
                if (s < 0) die("couldn't accept the relayed stream");
            }
        }
        struct pollfd p1 = { .fd = s, .events = POLLIN };
        if (poll(&p1, 1, 5000) != 1) die("nothing came over the stream");
        ssize_t n = recv(s, got, sizeof(got) - 1, 0);
        if (n <= 0) die("the stream closed");
        got[n] = 0;
        char ack[40];
        snprintf(ack, sizeof(ack), "ACK from %s", g_name);
        send(s, ack, strlen(ack), 0);
        printf("PASS %s: stream %s, got \"%s\"\n", g_name, via_relay ? "through the relay" : "direct", got);
        usleep(500 * 1000);
        close(s);
    }
    psnr_close(c);
    return 0;
}

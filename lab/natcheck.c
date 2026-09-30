/*
 * natcheck - one player in the NAT lab (lab/README.md). It does what the
 * runtime does for a title, without the title: says HELLO, probes the server
 * from its P2P socket so the server learns the socket's public endpoint,
 * hosts or joins a room, then punches and pings the other player at the
 * address the server handed out. Exits 0 once a PONG comes back.
 *
 *   natcheck <server> <name> host|join [p2p_port]
 *
 * POSIX only: the lab runs it in Linux containers.
 */
#include "../client/psnr.h"

#include <arpa/inet.h>
#include <fcntl.h>
#include <netinet/in.h>
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

/* A reply to request `req`, keeping psnr's heartbeat going meanwhile. */
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

    /* The server may still be starting. */
    uint32_t me = 0;
    uint8_t seen[4];
    psnr_client* c = NULL;
    for (int i = 0; i < 50 && !c; i++) {
        c = psnr_connect(server, 36100, COMM, g_name, p2p, &me, seen);
        if (!c) usleep(200 * 1000);
    }
    if (!c) die("can't reach the server");

    /* The P2P socket. */
    int u = socket(AF_INET, SOCK_DGRAM, 0);
    struct sockaddr_in local = { .sin_family = AF_INET, .sin_port = htons(p2p) };
    if (bind(u, (struct sockaddr*)&local, sizeof(local)) != 0) die("can't bind the P2P port");
    fcntl(u, F_SETFL, O_NONBLOCK);

    /* Probe until the server says where it sees the P2P socket. */
    struct sockaddr_in srv = { .sin_family = AF_INET };
    uint16_t sport;
    uint8_t pkt[64], ip[4];
    uint16_t pubport = 0;
    psnr_server_addr(c, (uint8_t*)&srv.sin_addr, &sport);
    srv.sin_port = htons(sport);
    psnr_probe_packet(c, pkt);
    for (double t0 = now(); !pubport; usleep(20 * 1000)) {
        if (now() - t0 > 5) die("no probe reply");
        sendto(u, pkt, PSNR_UDP_PROBE_LEN, 0, (struct sockaddr*)&srv, sizeof(srv));
        ssize_t n = recv(u, pkt + 32, 32, 0);
        if (n > 0 && psnr_probe_reply(pkt + 32, (size_t)n, ip, &pubport)) break;
        psnr_probe_packet(c, pkt);
    }
    printf("%s: P2P socket seen at %u.%u.%u.%u:%u\n", g_name, ip[0], ip[1], ip[2], ip[3], pubport);

    /* Meet the other player through a room. */
    uint8_t body[64], *p;
    psnr_msg m;
    uint32_t peer_user = 0;
    struct sockaddr_in peer;
    if (host) {
        p = body;
        *p++ = 2;                                    /* max */
        p = psnr_put32(p, 0);                        /* flags */
        p = psnr_put16(p, 0);                        /* external */
        p = psnr_put16(p, 0);                        /* internal */
        p = psnr_put16(p, 0);                        /* my data */
        m = call(c, PSNR_CREATE_ROOM, body, (uint32_t)(p - body));
        if (m.type != PSNR_ROOM_JOINED) die("create room failed");
        psnr_msg_free(&m);
        for (double t0 = now(); !peer_user; usleep(20 * 1000)) {
            if (now() - t0 > 30) die("nobody joined");
            int r = psnr_poll(c, &m);
            if (r < 0) die("lost the server");
            if (r == 1 && m.type == PSNR_MEMBER_JOINED) read_member(m.data + 8, &peer_user, &peer);
            if (r == 1) psnr_msg_free(&m);
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
        /* room u64 | mine u16 | owner u16 | max u8 | flags u32 | ext | int | count u8 | members */
        const uint8_t* q = m.data + 17;
        q += 2 + psnr_get16(q);
        q += 2 + psnr_get16(q);
        for (int i = 0, n = *q++; i < n; i++) {
            uint32_t user;
            struct sockaddr_in at;
            q += read_member(q, &user, &at);
            if (user != me) { peer_user = user; peer = at; }
        }
        psnr_msg_free(&m);
        if (!peer_user) die("the room has nobody else");
    }
    printf("%s: the other player is at %s:%u\n", g_name, inet_ntoa(peer.sin_addr), ntohs(peer.sin_port));

    /* Punch and ping until a PONG comes back. Keep answering PINGs for a
     * little while after, so the other side gets its PONG too. */
    uint8_t punch[PSNR_UDP_PUNCH_LEN];
    char buf[64], ping[40], pong[40];
    psnr_punch_packet(c, punch);
    snprintf(ping, sizeof(ping), "PING %s", g_name);
    snprintf(pong, sizeof(pong), "PONG %s", g_name);
    double done = 0, last = 0;
    for (double t0 = now(); !done || now() - done < 2; usleep(10 * 1000)) {
        if (!done && now() - t0 > 10) die("no PONG: the other player never got through");
        if (now() - last > 0.2) {
            sendto(u, punch, sizeof(punch), 0, (struct sockaddr*)&peer, sizeof(peer));
            sendto(u, ping, strlen(ping), 0, (struct sockaddr*)&peer, sizeof(peer));
            last = now();
        }
        struct sockaddr_in from;
        socklen_t fl = sizeof(from);
        ssize_t n = recvfrom(u, buf, sizeof(buf) - 1, 0, (struct sockaddr*)&from, &fl);
        if (n <= 0 || psnr_is_control(buf, (size_t)n)) continue;   /* the title never sees these */
        buf[n] = 0;
        if (!strncmp(buf, "PING", 4))
            sendto(u, pong, strlen(pong), 0, (struct sockaddr*)&from, fl);
        else if (!strncmp(buf, "PONG", 4) && !done) {
            printf("PASS %s: \"%s\" from %s:%u\n", g_name, buf, inet_ntoa(from.sin_addr),
                   ntohs(from.sin_port));
            done = now();
        }
        psnr_poll(c, &m) == 1 ? psnr_msg_free(&m) : (void)0;
    }
    psnr_close(c);
    return 0;
}

package main

import (
	"encoding/binary"
	"net"
	"testing"
	"time"
)

// startTCPUDP serves TCP and UDP on the same port, as main does.
func startTCPUDP(t *testing.T) (*Server, string) {
	s := NewServer(10, false)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	pc, err := net.ListenPacket("udp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close(); pc.Close() })
	go s.Serve(ln)
	go s.ServeUDP(pc)
	return s, ln.Addr().String()
}

// helloToken says HELLO and returns the user id and probe token.
func helloToken(t *testing.T, c *tc, name string, port uint16) (uint32, uint32) {
	w := &writer{}
	w.str("NPWR00001_00", 12)
	w.str(name, 16)
	w.u16(port)
	rt, r := c.call(msgHello, w.b)
	if rt != msgHelloAck {
		t.Fatalf("hello: 0x%02X", rt)
	}
	id := r.u32()
	r.take(4)
	return id, r.u32()
}

func probe(t *testing.T, addr string, id, token uint32, local [4]byte) (*net.UDPConn, []byte) {
	ua, _ := net.ResolveUDPAddr("udp", addr)
	u, err := net.DialUDP("udp", nil, ua)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { u.Close() })
	p := append([]byte(udpMagic), udpProbe)
	p = binary.BigEndian.AppendUint32(p, id)
	p = binary.BigEndian.AppendUint32(p, token)
	p = append(p, local[:]...)
	u.Write(p)
	u.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 64)
	n, err := u.Read(buf)
	if err != nil {
		return u, nil
	}
	return u, buf[:n]
}

// A probe with the right token is answered with the address it came from, and
// the server keeps it; a wrong token is ignored.
func TestProbeRecordsPublicEndpoint(t *testing.T) {
	s, addr := startTCPUDP(t)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	c := &tc{t: t, conn: conn}
	id, token := helloToken(t, c, "homer", 3658)

	if _, reply := probe(t, addr, id, token+1, [4]byte{192, 168, 1, 5}); reply != nil {
		t.Fatal("a probe with the wrong token was answered")
	}
	u, reply := probe(t, addr, id, token, [4]byte{192, 168, 1, 5})
	if len(reply) != 11 || string(reply[:4]) != udpMagic || reply[4] != udpProbeReply {
		t.Fatalf("probe reply %x", reply)
	}
	port := uint16(u.LocalAddr().(*net.UDPAddr).Port)
	if got := binary.BigEndian.Uint16(reply[9:]); got != port {
		t.Fatalf("reply says port %d, socket is on %d", got, port)
	}
	s.mu.Lock()
	cl := s.clients[id]
	s.mu.Unlock()
	if cl.udpPort != port || cl.udpIP != [4]byte{127, 0, 0, 1} || cl.localIP != [4]byte{192, 168, 1, 5} {
		t.Fatalf("recorded %v:%d local %v", cl.udpIP, cl.udpPort, cl.localIP)
	}
}

// Which address a member is given depends on who is looking.
func TestEndpointPerViewer(t *testing.T) {
	home := &client{ip: [4]byte{203, 0, 113, 7}, p2pPort: 3658,
		udpIP: [4]byte{203, 0, 113, 7}, udpPort: 40001, localIP: [4]byte{192, 168, 1, 5}}
	sameHouse := &client{ip: [4]byte{203, 0, 113, 7}}
	elsewhere := &client{ip: [4]byte{198, 51, 100, 9}}

	if ip, port := endpoint(home, elsewhere); ip != home.udpIP || port != 40001 {
		t.Fatalf("from another network: %v:%d, want the public mapping", ip, port)
	}
	if ip, port := endpoint(home, sameHouse); ip != home.localIP || port != 3658 {
		t.Fatalf("from the same network: %v:%d, want the LAN address", ip, port)
	}
	unprobed := &client{ip: [4]byte{127, 0, 0, 1}, p2pPort: 3659}
	if ip, port := endpoint(unprobed, elsewhere); ip != unprobed.ip || port != 3659 {
		t.Fatalf("no probe: %v:%d, want what TCP shows", ip, port)
	}
}

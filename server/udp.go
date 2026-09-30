package main

// UDP on the same port as TCP, for players behind NAT. A player's router
// gives their P2P socket a public port the server can't see over TCP, so the
// client's P2P socket sends a PROBE here and the server records where it came
// from: that public endpoint is what other players on other networks are
// given. Probes carry the token from HELLO_ACK, so nobody can move another
// player's endpoint. Peers also send PUNCH packets straight to each other to
// open their routers; receivers drop anything that starts with the magic.
//
// All packets: "PSNR" | type u8 | ...
//   PROBE        client -> server  user_id u32 | token u32 | local_ip [4]
//   PROBE_REPLY  server -> client  public_ip [4] | public_port u16
//   PUNCH        peer -> peer      user_id u32

import (
	"encoding/binary"
	"net"
)

const (
	udpMagic      = "PSNR"
	udpProbe      = 0x01
	udpPunch      = 0x02
	udpProbeReply = 0x81
)

func (s *Server) ServeUDP(pc net.PacketConn) error {
	buf := make([]byte, 65536)
	for {
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			return err
		}
		ua, ok := addr.(*net.UDPAddr)
		if ok && n >= 5 && string(buf[:4]) == udpMagic && buf[4] == udpRelay {
			s.relayUDP(pc, buf[:n])
			continue
		}
		if !ok || n < 17 || string(buf[:4]) != udpMagic || buf[4] != udpProbe {
			continue
		}
		ip := ua.IP.To4()
		if ip == nil {
			continue
		}
		id, token := binary.BigEndian.Uint32(buf[5:]), binary.BigEndian.Uint32(buf[9:])

		s.mu.Lock()
		c := s.clients[id]
		ok = c != nil && c.commID != "" && c.token == token
		if ok {
			copy(c.udpIP[:], ip)
			c.udpPort = uint16(ua.Port)
			copy(c.localIP[:], buf[13:17])
		}
		s.mu.Unlock()
		if !ok {
			continue
		}
		s.logf("client %d: P2P socket seen at %s, local %d.%d.%d.%d",
			id, ua, buf[13], buf[14], buf[15], buf[16])

		reply := append([]byte(udpMagic), udpProbeReply)
		reply = append(reply, ip...)
		reply = binary.BigEndian.AppendUint16(reply, uint16(ua.Port))
		pc.WriteTo(reply, addr)
	}
}

// endpoint is where viewer should send m's P2P traffic. From another network
// (a different public address), m's probed public endpoint; on the same
// network, m's local address, since most routers can't loop traffic from
// their LAN back to their own public address. Without a probe it falls back
// to what TCP shows: m's address and the P2P port m declared.
func endpoint(m, viewer *client) ([4]byte, uint16) {
	if m.udpPort != 0 && m.ip != viewer.ip {
		return m.udpIP, m.udpPort
	}
	if m.localIP != ([4]byte{}) {
		return m.localIP, m.p2pPort
	}
	return m.ip, m.p2pPort
}

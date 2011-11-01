package main

import (
        "log"
	"bytes"
	"fmt"
	"http"
	"os"
	"net"
	"time"
)

const ICMP_ECHO_REQUEST = 8
const ICMP_ECHO_REPLY = 0

// returns a suitable 'ping request' packet, with id & seq and a
// payload length of pktlen
func makePingRequest(id, seq, pktlen int, filler []byte) []byte {
	p := make([]byte, pktlen)
	copy(p[8:], bytes.Repeat(filler, (pktlen-8)/len(filler)+1))

	p[0] = ICMP_ECHO_REQUEST // type
	p[1] = 0                 // code
	p[2] = 0                 // cksum
	p[3] = 0                 // cksum
	p[4] = uint8(id >> 8)    // id
	p[5] = uint8(id & 0xff)  // id
	p[6] = uint8(seq >> 8)   // sequence
	p[7] = uint8(seq & 0xff) // sequence

	// calculate icmp checksum
	cklen := len(p)
	s := uint32(0)
	for i := 0; i < (cklen - 1); i += 2 {
		s += uint32(p[i+1])<<8 | uint32(p[i])
	}
	if cklen&1 == 1 {
		s += uint32(p[cklen-1])
	}
	s = (s >> 16) + (s & 0xffff)
	s = s + (s >> 16)

	// place checksum back in header; using ^= avoids the
	// assumption the checksum bytes are zero
	p[2] ^= uint8(^s & 0xff)
	p[3] ^= uint8(^s >> 8)

	return p
}

func parsePingReply(p []byte) (id, seq int) {
	id = int(p[4])<<8 | int(p[5])
	seq = int(p[6])<<8 | int(p[7])
	return
}

func handler (w http.ResponseWriter, r *http.Request) {
	if os.Getuid() != 0 {
		log.Fatal("test disabled; must be root")
	}

	dsthost := r.URL.Path[1:]
	srchost := ""

	log.Printf("Received request for %s", dsthost)
	log.Printf("Received request for %s", srchost)
	var (
		laddr *net.IPAddr
		err   os.Error
	)

	raddr, err := net.ResolveIPAddr(dsthost)
	if err != nil {
		fmt.Fprintln(w, `net.ResolveIPAddr("%v") = %v, %v`, dsthost, raddr, err)
		return
	}

	c, err := net.ListenIP("ip4:icmp", laddr)
	if err != nil {
		fmt.Fprintln(w, `net.ListenIP("ip4:icmp", %v) = %v, %v`, srchost, c, err)
	}

	sendid := os.Getpid() & 0xffff
	const sendseq = 61455
	const pingpktlen = 128
	sendpkt := makePingRequest(sendid, sendseq, pingpktlen, []byte("Go Go Gadget Ping!!!"))

	start := time.Nanoseconds()
	n, err := c.WriteToIP(sendpkt, raddr)
	if err != nil || n != pingpktlen {
		fmt.Fprintln(w, `net.WriteToIP(..., %v) = %v, %v`, raddr, n, err)
		return
	}

	c.SetTimeout(100e6)
	resp := make([]byte, 1024)
	for {
		n, from, err := c.ReadFrom(resp)
		if err != nil {
			fmt.Fprintln(w, `ReadFrom(...) = %v, %v, %v`, n, from, err)
			return
		}
		if resp[0] != ICMP_ECHO_REPLY {
			continue
		}
		rcvid, rcvseq := parsePingReply(resp)
		end := time.Nanoseconds()
		if rcvid != sendid || rcvseq != sendseq {
			fmt.Fprintln(w, `Ping reply saw id,seq=0x%x,0x%x (expected 0x%x, 0x%x)`, rcvid, rcvseq, sendid, sendseq)
			return
		}
		fmt.Fprintln(w, "response took %d nanoseconds.", end - start)
		return
	}
	fmt.Fprintln(w, "saw no ping return")
}

func main() {
	http.HandleFunc("/", handler)
	http.ListenAndServe(":8081", nil)
}


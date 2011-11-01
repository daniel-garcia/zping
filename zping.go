// Copyright (c) 2011, Daniel Garcia
// All rights reserved.
// 
// This program is licensed under the BSD License.
// See LICENSE file for details. 

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


type PingResponse struct {
	dsthost string
	rtt int64
}

type PingRequest struct {
	dsthost string
	responseChannel chan PingResponse
}


func parsePingReply(p []byte) (id, seq int) {
	id = int(p[4])<<8 | int(p[5])
	seq = int(p[6])<<8 | int(p[7])
	return
}


func PingPoller (service chan PingRequest) {

	srchost := ""
	for {
		r := <- service
		log.Printf("Received request for %s", r.dsthost)
		var (
			laddr *net.IPAddr
			err   os.Error
		)

		raddr, err := net.ResolveIPAddr(r.dsthost)
		if err != nil {
			log.Println(`net.ResolveIPAddr("%v") = %v, %v`, r.dsthost, raddr, err)
			reply := PingResponse{r.dsthost, -1}
			r.responseChannel <- reply
			continue
		}

		c, err := net.ListenIP("ip4:icmp", laddr)
		if err != nil {
			log.Println(`net.ListenIP("ip4:icmp", %v) = %v, %v`, srchost, c, err)
			reply := PingResponse{r.dsthost, -1}
			r.responseChannel <- reply
			continue
		}

		sendid := os.Getpid() & 0xffff
		const sendseq = 61455
		const pingpktlen = 128
		sendpkt := makePingRequest(sendid, sendseq, pingpktlen, []byte("Go Go Gadget Ping!!!"))

		start := time.Nanoseconds()
		n, err := c.WriteToIP(sendpkt, raddr)
		if err != nil || n != pingpktlen {
			log.Println(`net.WriteToIP(..., %v) = %v, %v`, raddr, n, err)
			reply := PingResponse{r.dsthost, -1}
			r.responseChannel <- reply
			continue
		}

		c.SetTimeout(100e6)
		resp := make([]byte, 1024)
		for {
			n, from, err := c.ReadFrom(resp)
			if err != nil {
				log.Println(`ReadFrom(...) = %v, %v, %v`, n, from, err)
				reply := PingResponse{r.dsthost, -1}
				r.responseChannel <- reply
				break
			}
			if resp[0] != ICMP_ECHO_REPLY {
				continue
			}
			rcvid, rcvseq := parsePingReply(resp)
			end := time.Nanoseconds()
			if rcvid != sendid || rcvseq != sendseq {
				log.Println(`Ping reply saw id,seq=0x%x,0x%x (expected 0x%x, 0x%x)`, rcvid, rcvseq, sendid, sendseq)
				reply := PingResponse{r.dsthost, -1}
				r.responseChannel <- reply
				break
			}
			log.Println("response took %d nanoseconds.", end - start)
			reply := PingResponse {r.dsthost, end - start}
			r.responseChannel <- reply
			break
		}
		log.Println("saw no ping return")
		reply := PingResponse{r.dsthost, -1}
		r.responseChannel <- reply
	}
}

func handler (w http.ResponseWriter, r *http.Request) {

	responseChan := make(chan PingResponse)
	request := PingRequest { r.URL.Path[1:], responseChan }
	requestChan <- request
	var (
		response PingResponse
	)
        response = <- request.responseChannel

	fmt.Fprintln(w, "response took %d nanoseconds.", response.rtt)
}

var (
	requestChan chan PingRequest
)

func main() {
	if os.Getuid() != 0 {
		log.Fatal("test disabled; must be root")
	}
	requestChan = make(chan PingRequest)
	go PingPoller(requestChan)
	http.HandleFunc("/", handler)
	http.ListenAndServe(":8081", nil)
}


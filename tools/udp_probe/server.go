package main

import (
	"fmt"
	"log"
	"net"
)

const port = 9999

func main() {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		log.Fatal(err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	fmt.Printf("UDP echo server listening on 0.0.0.0:%d\n", port)

	buf := make([]byte, 4096)
	for {
		n, remote, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Println("read err:", err)
			continue
		}
		_, _ = conn.WriteToUDP(buf[:n], remote)
	}
}

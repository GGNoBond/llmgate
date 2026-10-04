package tinytests

import (
	"fmt"
	"io"
	"net"
)

func SocketListen() {
	listen, err := net.Listen("tcp", ":8888")
	if err != nil {
		panic(err)
	}
	defer listen.Close()

	for {
		conn, err := listen.Accept()
		if err != nil {
			panic(err)
		}
		go func(conn net.Conn) {
			defer conn.Close()
			fmt.Printf("Connection established: %s\n", conn.RemoteAddr().String())
			buf := make([]byte, 1024)
			for {
				clear(buf)
				n, err := conn.Read(buf)
				if n > 0 {
					fmt.Println(string(buf))
					conn.Write([]byte("HTTP/1.0 200 OK\r\n\r\n"))
				}
				if err != nil {
					if err == io.EOF {
						fmt.Printf("Connection closed: %s\n", conn.RemoteAddr().String())
					} else {
						fmt.Println("Error reading from connection:", err)
					}
					conn.Close()
					return
				}
			}
		}(conn)
	}
}

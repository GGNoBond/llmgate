package tinytests

import (
	"fmt"
	"net"
	"os"
)

func SocketConn() {
	conn, err := net.Dial("tcp", "www.baidu.com:80")
	if err != nil {
		panic(err)
	}
	defer func(conn net.Conn) {
		err := conn.Close()
		if err != nil {
			fmt.Println("Error closing connection:", err)
		}
	}(conn)
	_, err = conn.Write([]byte("GET / HTTP/1.0\r\n\r\n"))
	if err != nil {
		panic(err)
	}
	var buf [512]byte
	n, err := conn.Read(buf[:])
	if err != nil {
		panic(err)
	}
	fmt.Println(string(buf[:n]))
	os.Exit(0)
}

package tinytests

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
)

func HTTPGet() {
	resp, err := http.Get("http://www.baidu.com")
	if err != nil {
		panic(err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			panic(err)
		}
	}(resp.Body)

	fmt.Printf("Response Status: %s", resp.Status)

	scanner := bufio.NewReader(resp.Body)
	for range 5 {
		line, err := scanner.ReadString('>')
		if err != nil {
			panic(err)
		}
		fmt.Println(line)
	}
}

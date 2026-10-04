package main

import (
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"slices"
	"strings"
	"time"
)

func add(a, b, c int) int {
	return a + b + c
}
func swap(a, b string) (string, string) {
	return b, a
}

func split(sum int) (a, b int) {
	a = sum / 10
	b = sum % 10
	return
}

const (
	Big   = 1 << 100
	Small = Big >> 99
)

func loop() {
	i := 0
	for {
		fmt.Println("i = ", i)
		i++
		if ii := i * i; ii > 20 {
			break
		}
	}
}

type ErrorNegativeSqrt float64

func (e ErrorNegativeSqrt) Error() string {
	return fmt.Sprintf("cannot Sqrt negative number: %v", float64(e))
}
func Sqrt(x float64) (float64, error) {
	if x < 0 {
		return 0, ErrorNegativeSqrt(x)
	}
	z := float64(1)
	for {
		z -= (z*z - x) / (2 * z)
		if math.Abs(z*z-x) < 1e-6 {
			return z, nil
		}
	}
}
func deferExample() {
	fmt.Println("output:")
	for _, c := range "hello" {
		fmt.Println(string(c))
	}
}

type Vertex struct {
	X, Y int
}

// dx: x range dy: y range
func Pic(dx, dy int) [][]uint8 {
	pic := make([][]uint8, dy)
	for y := range dy {
		pic[y] = make([]uint8, dx)
		for x := range dx {
			pic[y][x] = 1
		}
	}
	return pic
}
func WordCount(s string) map[string]int {
	ret := make(map[string]int)
	// for start := 0; start < len(s); {
	// 	if isLetter(s[start]) {
	// 		end := start + 1
	// 		for ; end < len(s) && isLetter(s[end]); end++ {
	// 		}
	// 		word := s[start:end]
	// 		ret[word]++
	// 		start = end + 1
	// 	}
	// }
	for _, value := range strings.Fields(s) {
		ret[value]++
	}
	return ret
}

type rot13Reader struct {
	r io.Reader
}

func (r rot13Reader) Read(b []byte) (int, error) {
	n, err := r.r.Read(b)
	r13 := func(c byte) byte {
		if c >= 'a' && c <= 'z' {
			return (c-'a'+13)%26 + 'a'
		} else if c >= 'A' && c <= 'Z' {
			return (c-'A'+13)%26 + 'A'
		} else {
			return c
		}
	}
	for i := 0; i < n; i++ {
		b[i] = r13(b[i])
	}
	return n, err
}
func Index[T comparable](tArr []T, t T) int {
	for i, v := range tArr {
		if v == t {
			return i
		}
	}
	return -1
}

// 1) 中间件类型：吃一个 Handler，吐一个"包着它"的新 Handler
type Middleware func(http.Handler) http.Handler

// 2) Chain：由内向外把中间件套成洋葱
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for _, mw := range slices.Backward(mws) {
		h = mw(h)
	}
	return h
}

// 3) 一个示例中间件：打印前置/后置，方便观察顺序
func Timing(name string) Middleware {
	return func(next http.Handler) http.Handler {
		// http.HandlerFunc 把普通函数变成 Handler
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Printf("[%s] 前置\n", name)
			start := time.Now()
			next.ServeHTTP(w, r) // ★ 交给下一层（洋葱往里走）
			fmt.Printf("[%s] 后置 (耗时 %v)\n", name, time.Since(start))
		})
	}
}

func Timing2(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("Timing2 前置\n")
		next.ServeHTTP(w, r)
		fmt.Printf("Timing2 后置\n")
	})
}

// 4) 核心业务 handler：一个自定义 struct，只要有 ServeHTTP 方法，它"就是"一个 Handler
type HelloHandler struct{}

func (h HelloHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fmt.Println("    [核心] 处理业务")
	w.Write([]byte("hello from core handler"))
}
func main() {
	// fmt.Println("Hello, World!")
	// fmt.Println("This time is ", time.Now())
	// fmt.Println("My favorite num is ", rand.Intn(10))
	// fmt.Println("Math Π: ", math.Pi)
	// fmt.Println("func add 1+2+3= ", add(1, 2, 3))
	// a, b := swap("hello", "world")
	// fmt.Println("swap hello world= ", a, b)
	// c, d := split(17)
	// fmt.Println("split 17= ", c, d)
	// e := float32(3.33)
	// fmt.Printf("My type is %T.\n", e)
	// // const world := "世界"  非法
	// // loop()
	// // fmt.Println(Sqrt(2), "real z:", math.Sqrt(2))
	// // deferExample()
	// var v *Vertex = &Vertex{Y: 22}
	// fmt.Println("Vetex X:", (*v).X, "Y:", v.Y)
	// arr := []int{1, 2, 3, 4}
	// arr[1], arr[2], arr[3] = 11, 22, 33
	// fmt.Println("Array arr:", arr[0:4], "size:", len(arr))
	// m := make(map[string]string)
	// m["aaa"] = "aaaaaaaaa"
	// m["aaab"] = ""
	// if elem, ok := m["aaal"]; ok {
	// 	fmt.Println("m[key] is not nil", elem)
	// }
	// for key, value := range m {
	// 	fmt.Println("map key:", key, "value:", value)
	// }
	// s := strings.NewReader("Lbh penpxrq gur pbqr!")
	// r := rot13Reader{s}
	// io.Copy(os.Stdout, &r)
	// fmt.Println()
	// vArr := []Vertex{Vertex{1, 2}, Vertex{3, 4}}
	// vv := Vertex{3, 4}
	// fmt.Println(Index(vArr, vv))

	// 用两层中间件包住核心 handler：Timing("A") 在最外，Timing("B") 次之
	h := Chain(HelloHandler{}, Timing("A"), Timing("B"), Timing2)

	mux := http.NewServeMux()
	mux.Handle("/hello", h) // 注册：只是存进路由表，此刻不执行

	log.Println("监听 :8080 —— 另开终端执行: curl http://localhost:8080/hello")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

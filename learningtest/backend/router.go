package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/aoxin/llmgate/learningtest/tinytests"
)

func getRouter() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/index", http.StatusMovedPermanently)
	})
	mux.HandleFunc("GET /index", indexPage)
	mux.HandleFunc("GET /hello", helloWorld)
	mux.HandleFunc("GET /time", timePage)
	mux.HandleFunc("GET /jsontest", getJsonTest)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir(
		func() string {
			root, err := tinytests.FindProjectRoot()
			if err != nil {
				panic(fmt.Sprintf("Find project root failed: %v", err))
			}
			return filepath.Join(root, "resource")
		}()))))

	return mux
}

func indexPage(w http.ResponseWriter, r *http.Request) {
	root, err := tinytests.FindProjectRoot()
	if err != nil {
		panic(fmt.Sprintf("Find project root failed: %v", err))
	}
	http.ServeFile(w, r, filepath.Join(root, "resource/index.html"))
}
func helloWorld(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hello from %s\n", r.RemoteAddr)
}
func timePage(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Current time: %s\n", time.Now().Format(time.RFC1123))
}
func getJsonTest(w http.ResponseWriter, r *http.Request) {
	jsonByte, err := json.MarshalIndent(loadConfig(""), "", "  ")
	if err != nil {
		tinytests.Error("Marshal JSON failed: %v", err)
		return
	}
	/***  反序列化
	var cfg *Config
	err := json.Unmarshal(jsonByte, &cfg)
	*/
	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonByte)

	/*** protobuf序列号测试
	cfgProto := &pb.Config{
		Server: &pb.ServerConfig{
			Name: cfg.Server.Name,
			Port: int32(cfg.Server.Port),
		},
		Logger: &pb.LoggerConfig{
			LogLevel:  int32(cfg.Logger.LogLevel),
			LogDir:    cfg.Logger.LogDir,
			LogOutput: cfg.Logger.LogOutput,
		},
	}

	cfgProtoByte, err := proto.Marshal(cfgProto)
	if err != nil {
		Error("Marshal Protocol Buffers failed: %v", err)
		return
	}
	Debug("cfgProtoByte: %v", cfgProtoByte)
	var cfgProtoRecv pb.Config
	err = proto.Unmarshal(cfgProtoByte, &cfgProtoRecv)

	if err != nil {
		Error("Unmarshal Protocol Buffers failed: %v", err)
		return
	}
	Debug("ProtoServer: %s %d", cfgProtoRecv.Server.Name, cfgProtoRecv.Server.Port)
	Debug("ProtoLogger: %d %s %v", cfgProtoRecv.Logger.LogLevel, cfgProtoRecv.Logger.LogDir, cfgProtoRecv.Logger.LogOutput)
	*/

	return
}

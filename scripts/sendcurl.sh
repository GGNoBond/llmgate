for i in $(seq 10); do
  curl -s http://localhost:8080/v1/chat/completions -H "Content-Type: application/json" \
    -d '{"model":"qwen2.5:1.5b","messages":[{"role":"user","content":"100字介绍你的功能"}],"stream":true}' > /dev/null
#  sleep 2
done

#CONCURRENCY=${1:-10}   # 默认并发 10，可用第一个参数覆盖：./sendcurl.sh 50
#
#for i in $(seq "$CONCURRENCY"); do
#  curl -s http://localhost:8080/v1/chat/completions -H "Content-Type: application/json" \
#    -d '{"model":"qwen2.5:1.5b","messages":[{"role":"user","content":"100字介绍你的功能"}],"stream":true}' > /dev/null &
#done
#
#wait   # 阻塞直到所有后台 curl 进程结束
#echo "all $CONCURRENCY requests done"
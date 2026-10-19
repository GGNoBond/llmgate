-- bench/post.lua : wrk 压测脚本，POST /v1/chat/completions（非流式，测网关转发吞吐）
-- 用法：wrk -t8 -c50 -d30s --latency -s bench/post.lua http://localhost:8080/v1/chat/completions
-- 参数须与 config/config.bench.native.yaml 对齐：模型 bench-gpt、key sk-llmgate-bench
wrk.method  = "POST"
wrk.path    = "/v1/chat/completions"
wrk.headers["Content-Type"]  = "application/json"
wrk.headers["Authorization"] = "Bearer sk-llmgate-bench"

local counter = 0

-- init 每个 wrk 线程各调用一次：错开随机种子，避免多线程随机串雷同
function init(args)
    math.randomseed(os.time() + counter * 100003)
    counter = counter + 1
end

function request()
    -- 每请求唯一 content：即便缓存开着也不会命中，保证压到“转发”路径
    local seq  = counter * 1000000000 + math.random(1, 1000000000)
    counter = counter + 1
    local body = string.format(
        '{"model":"bench-gpt","stream":false,"messages":[{"role":"user","content":"bench-%d"}]}',
        seq)
    return wrk.format(nil, nil, nil, body)
end

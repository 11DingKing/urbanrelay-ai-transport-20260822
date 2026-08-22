# BENZHI_README

## 项目说明

- 项目：11DingKing/urbanrelay-ai-transport-20260822
- 项目用途：UrbanRelay 面向道路运行协调员、低空巡检调度员、快递枢纽主管、多式联运协调员和安全审计员，统一管理无人配送任务、无人机巡检、分拣波次、铁公水衔接和交通异常处置。AI 识别结果只作为待核验业务输入，关键状态变化仍由有权限的业务角色确认。
- Go 工具链：`golang:1.26`
- 前端工具链：无

## 标准构建、运行和测试命令

进入容器后执行：

```bash
# 编译
cd '/app' && GOTOOLCHAIN=local go build ./...

# 启动
cd '/app' && GOTOOLCHAIN=local go run ./cmd/server

# 测试
cd '/app' && GOTOOLCHAIN=local go test ./...
```

## Docker 构建和进入容器

```bash
chmod +x build_benzhi_docker.sh
./build_benzhi_docker.sh benzhi-task-21-amd64 linux/amd64
./build_benzhi_docker.sh benzhi-task-21-arm64 linux/arm64
docker run -it benzhi-task-21-amd64:latest
docker run -it --platform linux/arm64 benzhi-task-21-arm64:latest
```

## 题目验证命令

1. 预期退出码 1：`go test ./internal/catalog -run '^TestAnnotationTransport21$' -count=1`

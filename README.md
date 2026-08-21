# UrbanRelay 城市立体运力协同平台

UrbanRelay 面向道路运行协调员、低空巡检调度员、快递枢纽主管、多式联运协调员和安全审计员，统一管理无人配送任务、无人机巡检、分拣波次、铁公水衔接和交通异常处置。AI 识别结果只作为待核验业务输入，关键状态变化仍由有权限的业务角色确认。

平台使用 Go 1.26、SQLite WAL 和真实 migrations。登录会话持久化、可撤销并有明确过期时间；关键写入带幂等键、乐观版本、事务审计与 outbox 事件。后台 worker 支持取消、退避重试、永久失败登记和优雅停止。

## 核心流程

- 无人配送：计划、资源预留、派发、执行、完成或取消，车辆同一时间只能承担一个有效任务。
- 无人机巡检：申请空域窗口、派发、证据回传、复核，未完成复核不能释放关联告警。
- 枢纽分拣：创建波次、预留分拣口、执行、核对差异、关闭，失败写入可重试作业。
- 多式联运：建立接驳计划、锁定承运段、确认交接、异常改派和完成，跨段状态在事务内一致变化。
- 安全处置：接收感知告警、人工分派、现场处置、复核关闭，审计记录关联操作者和请求 ID。

## 运行

复制环境变量后执行 "go run ./cmd/server"。默认监听 ":8080"，"GET /healthz" 为存活检查，"GET /readyz" 会检查数据库。演示账号为 "platform_admin/admin-demo-password"、"transport_dispatcher/dispatcher-demo-password"、"field_operator/field-demo-password" 和 "safety_auditor/auditor-demo-password"。

验证命令："go test ./... -count=1"、"go test -race ./... -count=1"、"go vet ./..."、"go build ./..."。


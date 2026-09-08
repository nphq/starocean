# StarOcean 家庭 / 单机部署方案（SQLite，零容器）

> 架构：Debian VM + 单二进制 + SQLite 文件 + systemd，可选 Cloudflare Tunnel 对外。
> 无 Docker、无 compose、无外部数据库。

```
浏览器 ──(可选)──▶ cloudflared Tunnel ──▶ 127.0.0.1:8080 ./starocean serve
                                               │
                                          data/starocean.db
```

## 1. 前置条件

- 一台常开机的 Linux（Debian 12+ / Ubuntu 22.04+，1C1G 即够）
- 一个域名（仅公网访问时需要，配合 Cloudflare Tunnel；纯内网可跳过 §4）

## 2. 安装

```bash
# 拉代码并构建（需 Go 1.26+；或直接下载 Release 二进制）
git clone <repo> starocean && cd starocean
make build

# 首次写入演示数据（生产请改密码，见 §5）
./starocean seed
SECRET_KEY=$(openssl rand -base64 32) ./starocean serve
# 访问 http://<内网IP>:8080，演示账号 admin / 3dQAKbZHqP6P
```

## 3. systemd 常驻

```ini
# /etc/systemd/system/starocean.service
[Unit]
Description=StarOcean ERP
After=network.target

[Service]
User=starocean
WorkingDirectory=/opt/starocean
Environment=SECRET_KEY=<openssl rand -base64 32 的输出>
Environment=DATABASE_URL=sqlite:/opt/starocean/data/starocean.db
Environment=COMPANY_NAME=StarOcean
ExecStart=/opt/starocean/starocean serve
Restart=always

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl enable --now starocean
systemctl status starocean
```

## 4. （可选）Cloudflare Tunnel 公网访问

1. Cloudflare Dashboard → Zero Trust → Networks → Tunnels → 创建 Tunnel，复制 Token。
2. 本机安装 `cloudflared`，跑起来指向 `http://127.0.0.1:8080`：

```bash
cloudflared service install <TOKEN>
```

Tunnel 是出站连接：无需公网 IP、无需端口转发、无需路由器配置。

## 5. 日常运维

```bash
systemctl restart starocean     # 重启
journalctl -u starocean -f      # 看日志
```

### 备份与恢复（SQLite 就是一个文件）

```bash
# 备份（停服或配合 WAL checkpoint；最稳是先 stop）
systemctl stop starocean
cp /opt/starocean/data/starocean.db /opt/starocean/backups/starocean-$(date +%Y%m%d-%H%M%S).db
systemctl start starocean

# 恢复
systemctl stop starocean
cp /opt/starocean/backups/<file>.db /opt/starocean/data/starocean.db
systemctl start starocean
```

建议加一条 cron 每天凌晨备份一次。

### 更新部署

```bash
cd /opt/starocean && git pull && make build && systemctl restart starocean
# 启动时自动执行迁移；/readyz 可做健康检查
```

### 重置演示数据

```bash
./starocean seed -clear   # 清空商品后重灌（危险：生产勿用）
```

## 6. 何时需要 PostgreSQL

单机 SQLite 足够支撑到 ~GB 级数据与数十并发。只有出现以下情况才考虑 PG：

- 需要多实例同时写库（SQLite 单写者模型）
- 需要 PG 特有能力（复杂并发、逻辑复制、成熟运维生态）

切换只需设置 `DATABASE_URL=postgres://...` 并跑一次 `./starocean migrate`，应用代码零改动。

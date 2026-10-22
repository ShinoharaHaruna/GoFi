# GoFi - 简单、安全的文件分享

[English](README.md)

GoFi 是轻量、可自托管的文件分享服务，支持公开/私有上传、基于 token 的私有下载、短链和 Swagger 文档。PostgreSQL 保存 API key 与短链；文件内容保存在宿主机的 `./data` 中。

## 容器快速开始

需要 Docker Engine 和较新的 Compose 插件（v2 或更新版本）。Dockerfile 固定使用 **Go 1.26.10**，它处于受支持范围，已包含 [eMudhra 相关的 `crypto/x509` 修复](https://groups.google.com/g/golang-announce/c/66pojueaQek)。后续仍需跟进安全版本。本部署方案不升级业务依赖，也不修改数据库 schema。

### 1. 配置全新安装

```sh
git clone https://github.com/ShinoharaHaruna/GoFi.git
cd GoFi
if [ ! -e deploy/config.toml ]; then
  (umask 027; cp deploy/config.toml.template deploy/config.toml)
fi
sudoedit deploy/config.toml
```

容器配置使用显式 **TOML 文件**，不是纯环境变量配置：

```toml
GIN_MODE = "release"
GOFI_PORT = "8080"
GOFI_BASE_DIR = "/app/data"
DATABASE_URL = "postgres://gofi_user:gofi_local_dev@db:5432/gofi_db?sslmode=disable"
```

这里保留的是现有 Compose 的**开发默认值**，不代表推荐新公网部署使用这些凭据。连接信息必须与数据库的实际凭据一致，密码等字段中的特殊字符须进行 URL 编码。新公网安装应在初始化数据库前配置凭据；已有数据库的口令轮换是独立操作，修改 `POSTGRES_PASSWORD` 不会修改已初始化数据库的密码。容器间连接使用 **`db:5432`**，不能使用 `localhost:54320`。

`deploy/config.toml` 已被 gitignore，通过只读挂载提供为 `/app/config.toml`。根目录原有的 `config.toml` 不会被替换，继续供宿主机开发和迁移回滚使用。不要把密钥写入 build arguments 或镜像 tag。

### 2. 准备权限

镜像以固定的 **UID:GID `10001:10001`** 运行，没有以 root 启动后修改权限的脚本。对于**使用 rootful Docker、未开启 user-namespace remapping 的全新安装**：

```sh
sudo install -d -o 10001 -g 10001 -m 0750 ./data ./data/public ./data/private
sudo chown root:10001 deploy/config.toml
sudo chmod 0640 deploy/config.toml
```

这些命令可重复执行，不会覆盖配置，但不会修复已有文件的权限。如果 `data` 已有上传文件，请使用[迁移步骤](#首次从-screen-迁移现有生产环境)中的停写、备份和权限处理流程。不要使用 `chmod 777`。Rootless Docker 或 `userns-remap` 会映射数字 ID，不能直接套用上述宿主机 ID，必须在对应 daemon 的命名空间中准备映射后的所有权。下方隔离测试会用一次性 fixture helper 兼容这两种情况，不会让业务应用以 root 启动。

### 3. 构建并启动整套服务

```sh
export GOFI_VCS_REF="$(git describe --always --dirty)"
docker compose -p gofi build app
docker compose -p gofi up -d --no-build --wait --wait-timeout 120
docker compose -p gofi ps
curl --fail http://127.0.0.1:8080/health
```

默认仍为**所有宿主机接口上的 `8080:8080`**，没有顺带收缩原有公网暴露范围。PostgreSQL 仍映射至 **`127.0.0.1:54320`**。测试鉴权操作前需创建 API key。不要启动第二个实例连接生产数据库，即使使用不同 HTTP 端口：应用在**监听端口前执行 AutoMigrate**。

通用配置会自动创建逻辑卷 `gofi_db_data`，全新安装无需预先创建卷。project 为 `gofi` 时，物理卷名是 **`gofi_gofi_db_data`**；其他 project 会获得各自独立的卷。[生产 override](deploy/compose.production.yml) 则要求该既有物理卷已存在，否则报错；它**不适用于全新安装**。

## 配置与隔离覆盖参数

TOML 应显式提供全部四个键。容器内部端口保持 `8080`，使端口映射和镜像健康检查一致。

| TOML 键 | 容器值 | 用途 |
| --- | --- | --- |
| `GIN_MODE` | `release` | Gin 运行模式 |
| `GOFI_PORT` | `8080` | 容器内部 HTTP 端口 |
| `GOFI_BASE_DIR` | `/app/data` | 绑定宿主机的上传目录 |
| `DATABASE_URL` | `postgres://…@db:5432/…` | Compose 数据库连接 |

当前 Viper 会为已经带有 `GOFI_*` 的键再添加 `GOFI` 前缀，且仅调用 `AutomaticEnv` 不保证所有键进入 `Unmarshal`。不要依赖旧版环境变量表或纯环境变量部署。本方案优先使用显式 TOML，不顺手重构配置系统。

以下变量是 **Compose 插值参数**，并非应用自身的配置环境变量：

| 变量 | 默认值 | 用途 |
| --- | --- | --- |
| `GOFI_IMAGE` | `gofi:local` | 构建或加载的应用镜像 |
| `GOFI_VCS_REF` | `unknown` | 构建时的版本后缀，无需复制 `.git` |
| `GOFI_CONFIG_FILE` | `./deploy/config.toml` | 只读挂载的宿主机配置路径 |
| `GOFI_DATA_DIR` | `./data` | 宿主机上传目录 |
| `GOFI_APP_PUBLISH` | `8080` | 宿主机 `[IP:]端口`，例如 `127.0.0.1:18080` |
| `GOFI_DB_PUBLISH` | `127.0.0.1:54320` | PostgreSQL 的宿主机 `[IP:]端口`，例如 `127.0.0.1:54321` |

隔离实例必须覆盖**全部状态输入**，不能只换端口：应使用独立的 `-p` project、配置文件、存储目录和数据库卷，不加载生产 override。`scripts/test-compose.py` 会自动创建临时状态并使用随机宿主机端口。绑定源路径须预先存在；Compose 不会在配置文件缺失时静默创建同名目录。

## 宿主机开发：仅启动 PostgreSQL

在独立的本地开发 checkout 中，使用 Go 1.26.10 或更新、受支持且包含修复的版本。`go.mod` 保留原有最低语言版本，本次不升级业务依赖。

```sh
GOFI_DB_PUBLISH=127.0.0.1:54321 docker compose -p gofi-dev up -d db
if [ ! -e config.toml ]; then
  (umask 027; cp config.toml.template config.toml)
fi
mkdir -p ./data/public ./data/private
```

保留已有宿主机配置。新建开发配置的示例：

```toml
GIN_MODE = "debug"
GOFI_PORT = "8080"
GOFI_BASE_DIR = "./data"
DATABASE_URL = "postgres://gofi_user:gofi_local_dev@localhost:54321/gofi_db?sslmode=disable"
```

```sh
go run ./cmd/gofi -config ./config.toml
```

`up -d db` 只启动数据库；不带服务名的 `up -d` 现在会启动**两个服务**。宿主机进程连接 `localhost` 和映射后的数据库端口，容器连接 `db:5432`。开发不得共享生产数据库/存储，也不得让宿主机应用和容器应用同时连接同一生产数据库。

## API 使用与首次密钥

Swagger UI：<http://localhost:8080/swagger/index.html>。

| 接口 | 访问规则 |
| --- | --- |
| `GET /health` | 无需鉴权，仅检查 HTTP liveness |
| `POST /upload` | 需要 `upload` token；`X-GoFi-Target-Dir: public` 选择公开目录，否则为私有 |
| `GET /:filename` | 公开文件匿名访问；私有文件需要 `download` token |
| `POST /shorten` | 需要 `shorten` token；JSON 为 `{"filename":"example.txt"}` |
| `GET /s/:shortcode` | 公开短链匿名访问；私有短链需要 `download` token |
| `DELETE /shorten/:shortcode` | 需要 `shorten` token；停用短链 |
| `POST /shorten/:shortcode/enable` | 需要 `shorten` token；重新启用短链 |

首次启动应用并初始化表后，打开数据库会话，将占位符替换为不同的强随机密钥（不要把真实 key 粘贴进文档或 shell history）：

```sh
docker compose -p gofi exec db psql -U gofi_user -d gofi_db
```

```sql
INSERT INTO api_keys (key, type, is_enabled)
VALUES
  ('<your-upload-key>', 'upload', true),
  ('<your-download-key>', 'download', true),
  ('<your-shorten-key>', 'shorten', true);
```

通过 `Authorization: Bearer <key>` 发送 token。私有下载也接受 `?token=<key>`，但查询字符串可能泄漏至访问日志。生产已有的 key 和短链保存在既有数据库中；迁移时不重新初始化或替换它们。

## 日常操作

现有生产环境的**每条**命令应使用迁移章节定义的 `pc`，替代裸 `docker compose -p gofi`，从而始终启用 external volume 保护。

```sh
pc logs --tail 100 -f app db
pc stop app       # 只停止应用，保留运行中的 PostgreSQL。
pc stop           # 停止两个服务，保留全部状态。
pc down           # 删除容器和网络，保留命名卷与绑定目录。
```

**生产环境禁止执行 `docker compose down -v`。** 它可能删除 Compose 管理的数据库卷。External volume 是额外保护，不能代替备份，也不能阻止显式的 `docker volume rm` 或 prune。删除 `./data` 同样会丢失上传文件。

### 更新与镜像

在本地或 CI 机器构建，不要在约 1 GB RAM 的生产 VPS 上编译。构建使用 `CGO_ENABLED=0`、串行 package 编译、`GOMAXPROCS=2` 和 Go 软内存目标，**并非硬内存上限**。验收后传输与 VPS 架构一致的预构建镜像，不要在 VPS 命令中加入 `--build`。

```sh
# 先执行常规的 dirty-tree 验证，然后提交经审核的改动。
git status --short

docker compose -p gofi build app &&
  python3 scripts/test-compose.py --image gofi:local --audit-image

# 提交后，从该 commit 重建，并对同一镜像重新执行检查。
release_ref="$(git rev-parse --short HEAD)" &&
  export GOFI_VCS_REF="$release_ref" &&
  export GOFI_IMAGE="gofi:${release_ref}" &&
  docker compose -p gofi build --no-cache app &&
  python3 scripts/test-compose.py --image "$GOFI_IMAGE" --audit-image &&
  docker image save --output "gofi-${release_ref}.tar" "$GOFI_IMAGE" &&
  git push origin main
```

没有 Git 的源码归档应显式设置 `GOFI_VCS_REF`，否则后缀为 `unknown`。二进制启动日志中的版本为 `<VERSION>-<revision>`，沿用现有约定。最终镜像只包含运行时系统、CA 证书、curl 和二进制，不包含构建阶段、模块缓存、源码、上传文件或配置文件。构建上下文 allowlist 与源码专用 `COPY` 路径共同排除本地密钥；新增 Go package 时需同步更新这些列表。

不要把 dirty 或提交前验证的镜像改名后发布：其二进制版本和 `org.opencontainers.image.revision` label 已经写入旧 revision。要求顺序是**修订并验证 → 正式 commit → 从该 commit 重建 → 复验同一镜像 → push → VPS `git pull --ff-only` → 传输并加载同一镜像**。保留旧镜像和宿主机二进制，在停写并备份后执行：

```sh
# 生产环境，已定义下方 pc，且已确认现有 db 健康。
project_root="$(pwd -P)" &&
  resolved_config="$(realpath "$project_root/deploy/config.toml")" &&
  resolved_data="$(realpath "$project_root/data")" &&
  export GOFI_CONFIG_FILE="$resolved_config" &&
  export GOFI_DATA_DIR="$resolved_data" &&
  release_ref="$(git rev-parse --short HEAD)" &&
  export GOFI_IMAGE="gofi:${release_ref}" &&
  docker image load --input /path/to/gofi-image.tar &&
  deploy/verify-production.sh &&
  pc up -d --no-deps --no-build --pull never --wait --wait-timeout 120 app &&
  pc logs --tail 100 app
```

这些命令只替换 `app`，不拉取或重建 PostgreSQL。数据库 tag 保持 `postgres:17-bookworm`；迁移时不要顺便 pull/recreate `db`，因为可变 tag 可能改变实际运行的 patch 版本。PostgreSQL 升级和口令轮换属于独立变更。

### 一致性备份

必须同时备份 PostgreSQL（key 和短链）与 `./data`（文件内容），并保留两份配置、旧二进制/镜像引用和文件所有权元数据。备份放在仓库外，限制权限。在**所有**应用写入方均已停止（包括旧 screen 进程）的窗口中：

```sh
project_root="$(pwd -P)" &&
  resolved_config="$(realpath "$project_root/deploy/config.toml")" &&
  resolved_data="$(realpath "$project_root/data")" &&
  export GOFI_CONFIG_FILE="$resolved_config" &&
  export GOFI_DATA_DIR="$resolved_data" &&
  release_ref="$(git rev-parse --short HEAD)" &&
  export GOFI_IMAGE="gofi:${release_ref}" &&
  deploy/verify-production.sh &&
  umask 077 &&
  backup_dir="$(mktemp -d "${HOME}/gofi-backup.XXXXXX")" &&
  # 若旧 screen 进程仍在运行，仅 pc stop app 并不足够。
  pc stop app &&
  pc exec -T db pg_dump -U gofi_user -d gofi_db --format=custom > "$backup_dir/database.dump" &&
  sudo tar --numeric-owner --acls --xattrs -C "$resolved_data" -cpf "$backup_dir/uploads.tar" . &&
  sudo cp -p config.toml "$backup_dir/host-config.toml" &&
  sudo cp -p "$resolved_config" "$backup_dir/container-config.toml" &&
  sudo cp -p build/GoFi "$backup_dir/GoFi.previous"
```

验证 dump（`pg_restore --list`）、归档清单和恢复流程后再依赖备份。不要把运行中的 PostgreSQL 数据目录直接复制当作逻辑备份。数据库和文件快照都完成后，只启动选定的应用；迁移或恢复期间持续停写。备份包含密钥和私有文件，应妥善保护并异地保存。后续纯容器安装可能没有 `build/GoFi`，应保留上一版镜像。

## 首次从 screen 迁移：现有生产环境

**以下仅为验收后的 operator runbook；本次实现不授权，也不执行 VPS 访问。** 预期状态：project 为 `gofi`，数据库容器为 `gofi-db-1`，镜像引用 `postgres:17-bookworm`、实际 PostgreSQL 17.6，物理卷 `gofi_gofi_db_data` 挂载至 `/var/lib/postgresql/data`，数据库宿主机端口 `127.0.0.1:54320`，上传目录为 `/root/GoFi/data`，旧应用由 root 在 screen 中运行。发现不一致须停止调查，不得初始化空库替代。

1. 预先准备审核过的源码和镜像，不启动第二个应用实例。保留根目录 `config.toml` 和 `build/GoFi`。明确批准发布后通过 `git pull --ff-only` 同步源码，不 reset 有改动的工作区。加载本地构建且架构正确的镜像。仅在 `deploy/config.toml` 不存在时创建它，填写既有数据库的真实凭据，地址用 **`db:5432`**，模式用 `release`，端口用 `8080`，目录用 `/app/data`。旧宿主机配置继续保留 `localhost:54320`。不要先连生产库试运行：AutoMigrate 发生在端口冲突之前。

2. 所有生产操作均加载 override：

   ```sh
   cd /root/GoFi &&
     project_root="$(pwd -P)" &&
     resolved_config="$(realpath "$project_root/deploy/config.toml")" &&
     resolved_data="$(realpath "$project_root/data")" &&
     export GOFI_CONFIG_FILE="$resolved_config" &&
     export GOFI_DATA_DIR="$resolved_data" &&
     release_ref="$(git rev-parse --short HEAD)" &&
     export GOFI_IMAGE="gofi:${release_ref}" &&
     pc() { docker compose --env-file /dev/null -p gofi -f docker-compose.yml -f deploy/compose.production.yml "$@"; } &&
     deploy/verify-production.sh &&
     db_container_before="$(docker inspect --format '{{.Id}}' gofi-db-1)"
   ```

   `name: gofi` 配合 `-p gofi` 保留原有网络/project；逻辑卷键仍为 `gofi_db_data`。Override 将物理卷 `gofi_gofi_db_data` 声明为 external，卷缺失时应报错，而不是静默建空库。不要为了通过检查而新建同名卷。

3. 开始维护/停写窗口，让在途上传完成并停止客户端/其他写入方。连接旧 screen（`screen -r 2817.gofi`，或核实后的当前会话），用 Ctrl-C 停止旧 `build/GoFi`，确认进程和 `:8080` listener 都已消失。当前应用没有请求级 graceful shutdown，停止时可能中断在途请求。保持 PostgreSQL 运行，然后在任何备份或权限修改前 fail-fast 核验：

   ```sh
   deploy/verify-production.sh --stopped
   ```

   在**全部写入方停止后**执行并验证上方数据库、文件、配置与二进制备份。

4. 在停写窗口中，为既有上传文件准备非 root 权限。以下命令可重复执行、不跟随符号链接、不改变文件内容；先备份原始元数据：

   ```sh
   deploy/verify-production.sh --stopped &&
     sudo install -d -o 10001 -g 10001 -m 0750 "$resolved_data" "$resolved_data/public" "$resolved_data/private" &&
     sudo find "$resolved_data" -xdev -type d -exec chown 10001:10001 {} + -exec chmod 0750 {} + &&
     sudo find "$resolved_data" -xdev -type f -exec chown 10001:10001 {} + -exec chmod 0640 {} + &&
     sudo chown root:10001 "$resolved_config" &&
     sudo chmod 0640 "$resolved_config" &&
     deploy/verify-production.sh --permissions-ready
   ```

   这是显式的一次性宿主机准备，不是应用启动流程。若有意外的符号链接、特殊文件、独立挂载点或 ACL 需求，先检查再继续。如果 Docker 开启 user-namespace remapping，先调整 ID。Bind mount 会覆盖镜像目录的权限，仅修改镜像所有权无法修复宿主机 `root:root`、`0750` 目录的写入问题。

5. 在已确认数据库健康的前提下，**只启动应用**：

   ```sh
   deploy/verify-production.sh --permissions-ready &&
     pc up -d --no-deps --no-build --pull never --wait --wait-timeout 120 app &&
     pc ps &&
     pc logs --tail 100 app &&
     curl --fail http://127.0.0.1:8080/health &&
     test "$(docker inspect --format '{{.Id}}' gofi-db-1)" = "$db_container_before"
   ```

   `--no-deps` 特意不操作现有数据库，也跳过依赖启动检查，因此第 2 步必须显式确认健康。检查历史公开/私有下载和短链，再由 operator 使用既有 key 和一次性文件测试授权上传/下载。`/health` 单独通过不够。确认仍使用原数据库卷和宿主机上传目录后，再开放写入并结束维护窗口。不得将容器集成测试脚本指向生产。

### 回滚到 screen

1. 再次停写，结束或记录在途请求。执行 `pc stop app` 和 `pc rm -f app`，避免重启策略重新拉起竞争实例。**不要停止/删除 `db`，不要删除卷，不要执行 `down -v`。**
2. 如果容器已经接受写入，先再做一次配对的数据库/文件快照。默认回滚复用当前数据库和文件，包括迁移后新增的文件；不要直接用旧归档覆盖新数据。本次没有 schema 变更，但启动仍执行 AutoMigrate。如果确需恢复数据库，应先核对新写入，在全部写入方停止时将数据库和对应文件快照一起恢复。
3. 对于本文记录的原 root 运行方式，将数据所有权改回 `root:root`，覆盖迁移后新建的文件：

   ```sh
   resolved_data="$(realpath ./data)" &&
     sudo find "$resolved_data" -xdev -type d -exec chown 0:0 {} + -exec chmod 0750 {} + &&
     sudo find "$resolved_data" -xdev -type f -exec chown 0:0 {} + -exec chmod 0640 {} +
   ```

   若原始所有者、mode 或 ACL 不同，应按备份的元数据恢复。不要仅为改权限就还原整个文件归档。继续保护容器配置；旧应用使用未改动的根目录 `config.toml`，通过 `localhost:54320` 访问数据库。
4. 必要时恢复保存的旧二进制，以原 root 用户创建 screen（`screen -S gofi`），在 `/root/GoFi` 中执行 `./build/GoFi -config /root/GoFi/config.toml`。确认只有这一个实例，检查日志、下载、私有鉴权和短链后再开放写入。保留备份和已批准镜像以便排查。

## 健康检查与退出限制

- Compose 首次启动应用前等待数据库 `pg_isready` 通过；它不保证数据库持续可用，也不验证应用凭据/schema。
- `/health` 仅返回 `{"status":"UP"}`。镜像健康检查属于 HTTP **liveness（存活探测）**，不是完整业务 readiness（就绪探测）：不会查询数据库，也不会检查存储权限或容量。
- Docker 将容器标记为 **unhealthy 不会自动重启它**。`restart: unless-stopped` 针对进程退出，并在未被显式停止时允许 daemon/主机重启后拉起容器；它不会重启仍在运行但 unhealthy 的应用，仍需监控和人工处理。
- 当前没有 `http.Server.Shutdown` 或请求级 graceful draining。Exec-form entrypoint 能直接传递容器信号，但容器化不会自动保证在途请求完成。
- 旧版本公开短链缺陷留下的历史记录可能仍为 `is_private=true`，而文件实际位于 `public/`。匿名访问返回 401；携带有效 `download` token 后会改查 `private/` 路径并返回 404。新建短链现在会持久化正确值；系统不会自动批量改写旧记录。修复受影响记录前应先人工核实，可仅将已确认的短码设置为 `is_private=false`。

## 本地验证

仅在开发机器执行，不挂载生产配置：

```sh
GOTOOLCHAIN=go1.26.10 go test ./...
scripts/test-production-gate.sh
docker compose --env-file /dev/null config -q
docker compose --env-file /dev/null -p gofi -f docker-compose.yml -f deploy/compose.production.yml config -q
GOFI_VCS_REF="$(git describe --always --dirty)" GOFI_IMAGE=gofi:verify \
  docker compose --env-file /dev/null -p gofi-build build app
python3 scripts/test-compose.py --image gofi:verify --audit-image
```

测试脚本仅依赖 Python 3 标准库，内部生成 project 名，使用临时 TOML/存储、独立命名数据库卷和随机 loopback 端口，检查非 root 身份、挂载、健康接口、公私有鉴权、上传/下载、短链的持久化隐私值、app 重建后的持久化和最终镜像内容。`scripts/test-production-gate.sh` 是纯 stub 负例，验证核验失败后不会执行文档中的备份、chown 和启动链。一次性的 root fixture helper **只调整临时测试文件**，兼容 rootful/rootless 的 ID 映射。脚本仅清理自身生成的 project 和测试卷；这不是生产清理配方。如果没有 Docker 或无权访问，Go 测试与 Compose 解析通过也不代表镜像构建/集成测试通过，应明确标记为**未验证**。

## 许可证

[MIT](LICENSE)。

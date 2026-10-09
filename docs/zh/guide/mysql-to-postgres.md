# 从 MySQL 迁移到 PostgreSQL

当前版本已经支持 `DB_TYPE=postgres`，无需修改业务代码。本流程采用停机迁移，先演练，再正式切换。MySQL SQL 备份不能直接导入 PostgreSQL。

## 1. 准备和停写

- 使用与生产相同的 Kite 代码/镜像版本，不同时升级应用。
- 备份部署配置、Secret 和 MySQL。保留原来的 `KITE_ENCRYPT_KEY`、显式配置的 `JWT_SECRET`；数据库中的密文和密码哈希原样迁移，不经过应用接口重新创建。
- 记录 Kite 的副本数，停止所有副本及其它连接该库的写入程序。后台审批过期和通知任务也必须停止。
- PostgreSQL 使用全新、独立的数据库，例如 `kite_pg`，迁移账户拥有该库建表、索引、序列和外键权限。不要提前启动连接新库的完整 Kite，它会生成设置、角色、模板等初始数据。
- 下面示例假设源 MySQL 数据库名是 `kite`，历史业务时间是北京时间。实际名称、时区、地址、用户和密码必须替换。

在 Ubuntu 迁移机安装客户端（或使用已有工具）：

```bash
sudo apt-get update
sudo apt-get install pgloader postgresql-client default-mysql-client
umask 077
```

将 MySQL 连接信息放入权限为 `600` 的 `mysql-client.cnf`，避免命令行直接暴露密码：

```ini
[client]
host=MYSQL_HOST
port=3306
user=MIGRATION_USER
password=MYSQL_PASSWORD
```

停写后备份，检查命令成功并保留文件：

```bash
mysqldump --defaults-extra-file=./mysql-client.cnf \
  --single-transaction --quick --hex-blob kite > kite-before-pg.sql
```

源库预检查：

```bash
mysql --defaults-extra-file=./mysql-client.cnf kite <<'SQL'
SELECT @@global.time_zone, @@session.time_zone, @@system_time_zone;
SELECT id, created_at, updated_at, last_login_at FROM users ORDER BY id LIMIT 5;
SELECT id, created_at, approved_at, expires_at FROM access_requests ORDER BY id LIMIT 5;
SELECT table_name, column_name, column_type FROM information_schema.columns
WHERE table_schema = DATABASE()
  AND (data_type IN ('blob','tinyblob','mediumblob','longblob','binary','varbinary')
       OR column_name = 'id')
ORDER BY table_name, ordinal_position;
SQL
```

记录上述时间供迁移后比对。MySQL `DATETIME` 没有时区信息，不能仅凭服务器时区判断它的真实含义。如果历史数据存的是 UTC，需调整下文 PG `timezone`，并核对 MySQL `TIMESTAMP` 的读取时区。

若 `pending_sessions` 的消息/参数列仍是 BLOB，先在源库副本上核实它们是 UTF-8 JSON 文本，再将这些列转换为 `MEDIUMTEXT` 后迁移；不能使用 pgloader 默认的 BLOB→`bytea` 映射替代本版本的文本字段。不要未经核实转换其它二进制列。

## 2. 导入空 PostgreSQL 数据库

使用管理员创建 `kite_pg`，让迁移账户拥有它。确认数据库 `public` schema 中没有应用表。

复制本目录的 `mysql-to-postgres.load.example` 为本地 `kite.load`，替换连接信息及源库名。连接 URI 中用户名/密码的特殊字符必须进行 URL 编码。配置文件及 pgloader 日志可能含连接信息或业务数据，勿提交到 Git。

```bash
chmod 600 kite.load
pgloader kite.load
```

配置保留源 ID，并将自增 ID 建为 PG `bigserial`、其它无符号 `bigint` 建为 `bigint`，避免与 GORM 的 PG 模型类型不一致。超过 PG 有符号 bigint 上限的数据必须先处理；Kite 的普通自增 ID 通常不会接近这个范围。

多数时间列应为 `timestamptz`，`users.last_login_at` 则因当前模型显式指定 `timestamp` 而使用不带时区的类型。零日期转为 NULL；若源列有 NOT NULL 约束，非法零日期应先清理，不要用虚构时间掩盖问题。保留布尔值的默认转换，不要把所有 `tinyint` 强制转成布尔值。

成功标准是所有表导入完成、没有 rejected rows/转换错误，索引、外键和序列建立成功。失败的目标库可能已有部分数据，不能直接向它重复导入；修正原因后使用另一个空目标库重试。

## 3. 用当前模型核对目标结构，不启动业务

在同版本 Kite 仓库根目录运行。需要 Go 版本符合 `go.mod`，该步骤不会启动 HTTP、通知或审批任务，也不会创建默认用户/角色。

```bash
cat > /tmp/kite-pg-schema.go <<'GO'
package main

import (
    "os"
    "github.com/zxh326/kite/pkg/common"
    "github.com/zxh326/kite/pkg/model"
)

func main() {
    common.DBType = "postgres"
    common.DBDSN = os.Getenv("DB_DSN")
    if common.DBDSN == "" { panic("DB_DSN is required") }
    model.InitDB()
}
GO

# 设置目标 PG 的 DB_DSN，建议通过受保护的环境文件加载。
# 示例格式：host=PG_HOST port=5432 user=kite password=... dbname=kite_pg sslmode=require TimeZone=Asia/Shanghai
CGO_ENABLED=0 go run /tmp/kite-pg-schema.go
```

这一步会执行 `AutoMigrate`，因此必须先在演练库验证。若出现类型变更、外键、索引或权限错误，解决后再继续，不能忽略。

## 4. 校验

使用 `.pgpass` 或其它受保护方式配置 PG 客户端密码。下面 `psql` 示例不在命令行写密码。

MySQL 精确行数：

```bash
mysql --defaults-extra-file=./mysql-client.cnf -N -B kite -e \
  "SELECT CONCAT('SELECT ', QUOTE(table_name), ', COUNT(*) FROM ', CHAR(96), table_name, CHAR(96), ';') FROM information_schema.tables WHERE table_schema=DATABASE() AND table_type='BASE TABLE' ORDER BY table_name" \
  > /tmp/kite-counts-mysql.sql
mysql --defaults-extra-file=./mysql-client.cnf -N -B kite < /tmp/kite-counts-mysql.sql
```

PostgreSQL 精确行数：

```sql
-- 在 psql 中执行；\gexec 是 psql 命令。
SELECT format('SELECT %L AS table_name, count(*) FROM public.%I;', tablename, tablename)
FROM pg_tables WHERE schemaname = 'public' ORDER BY tablename
\gexec
```

当前模型生成 16 张表，包含 `user_group_members`，OAuth 表的实际名称为 **`o_auth_providers`**。旧版本可能缺少新表，以实际源表及本次 AutoMigrate 新增空表为准，逐一解释行数差异。

继续核对：

- 原有用户、用户组、角色及授权关联 ID 不变；API Key 包含用户 ID，不能重新编号。
- `clusters.config`、API Key、OAuth/LDAP/飞书凭据、设置中的 JWT 密钥等密文字段逐行比较摘要，不能仅比较行数。
- 比对先前记录的时间；PG 会话先设置 `SET TIME ZONE 'Asia/Shanghai';`。审批到期时间应代表同一时刻。
- 比对密码哈希、资源历史、长 YAML、AI 消息、NULL 与空字符串。不得重新哈希已有密码。
- 检查所有外键有效、无孤立关联记录。pgloader 的 `reset sequences` 应已执行；再用新建并删除测试记录的方式验证自增不会冲突。
- 导入后执行 `ANALYZE;`，更新查询统计信息。

## 5. 切换和回退

更新部署配置/Secret：

```yaml
DB_TYPE: postgres
DB_DSN: "host=PG_HOST port=5432 user=kite password=PG_PASSWORD dbname=kite_pg sslmode=require TimeZone=Asia/Shanghai"
TZ: Asia/Shanghai
# KITE_ENCRYPT_KEY 保持与原部署完全一致
# 原来显式配置的 JWT_SECRET 也保持一致
```

TLS 参数按实际 PG 服务设置；数据库类型必须是 `postgres`。Helm 使用 `db.type: postgres` 和 `db.dsn`；若使用 existingSecret，应更新 Secret 中的 `DB_TYPE`、`DB_DSN`。

先启动一个实例，检查启动迁移日志、普通/OAuth/LDAP 登录、旧 API Key、用户组/RBAC、集群连接、资源历史、审批、飞书通知和 AI 会话，再恢复原副本数。新实例启动后后台任务会恢复处理过期审批和待发送通知。

PG 常规排序规则下，搜索和用户名匹配可能比 MySQL 的不区分大小写排序规则更严格；验收必须覆盖现有账号的大小写使用方式，不要未经评估把权限主体统一小写。

PG 开始写入之前，可以停止新实例并切回原 MySQL 配置。PG 已产生新数据后，直接回退会丢失新增用户、审批、审计及通知状态；需先决定如何补回差异。保留 MySQL 备份和原部署配置到验收完成。

参考：[pgloader MySQL 导入选项](https://pgloader.readthedocs.io/en/latest/ref/mysql.html)、[PostgreSQL 时间类型](https://www.postgresql.org/docs/current/datatype-datetime.html)。

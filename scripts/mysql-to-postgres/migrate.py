"""Read a fresh MySQL consistent snapshot and transactionally replace PostgreSQL Kite data."""
import base64
import datetime as dt
import decimal
import gzip
import hashlib
import json
import pathlib
import sys
import time
import itertools
from zoneinfo import ZoneInfo

import argparse
import getpass
import os
import pymysql
import psycopg
from psycopg import sql

ROOT = pathlib.Path(__file__).resolve().parent
CONFIG = {}
WORK = ROOT
SNAPSHOT = None
COMMIT_STARTED = False
PHASE = 'initializing'
EXPECTED_TABLES = ['access_requests', 'clusters', 'feishu_notification_settings', 'general_settings', 'ldap_settings', 'o_auth_providers', 'pending_sessions', 'proxy_authorization_codes', 'proxy_sessions', 'resource_histories', 'resource_templates', 'role_assignments', 'roles', 'user_group_members', 'user_groups', 'users']
SHANGHAI = ZoneInfo('Asia/Shanghai')
UTC = dt.timezone.utc


def emit(value):
    print(value, flush=True)


def packed(value):
    if isinstance(value, bytes):
        return {'bytes': base64.b64encode(value).decode('ascii')}
    if isinstance(value, (dt.datetime, dt.date, dt.time)):
        return {'time': value.isoformat(), 'kind': type(value).__name__}
    if isinstance(value, decimal.Decimal):
        return {'decimal': str(value)}
    if isinstance(value, (list, tuple)):
        return [packed(v) for v in value]
    return value


def unpacked(value):
    if isinstance(value, list):
        return [unpacked(v) for v in value]
    if isinstance(value, dict):
        if 'bytes' in value:
            return base64.b64decode(value['bytes'])
        if 'time' in value:
            return getattr(dt, value['kind']).fromisoformat(value['time'])
        if 'decimal' in value:
            return decimal.Decimal(value['decimal'])
        raise ValueError('Unknown backup encoding')
    return value


def line(value):
    return json.dumps(packed(value), ensure_ascii=False, separators=(',', ':')).encode('utf-8') + b'\n'


def mysql_identifier(value):
    return '`' + value.replace('`', '``') + '`'


def source():
    con = pymysql.connect(**CONFIG['mysql'], autocommit=False, read_timeout=180)
    with con.cursor() as cur:
        cur.execute("SET SESSION time_zone = '+08:00'")
        cur.execute('SET SESSION TRANSACTION ISOLATION LEVEL REPEATABLE READ')
        cur.execute('START TRANSACTION WITH CONSISTENT SNAPSHOT, READ ONLY')
    return con


def rows(con, table, info):
    query = 'SELECT ' + ','.join(mysql_identifier(c) for c in info['columns'])
    query += ' FROM ' + mysql_identifier(table)
    query += ' ORDER BY ' + ','.join(mysql_identifier(c) for c in info['primary_key'])
    with con.cursor(pymysql.cursors.SSCursor) as cur:
        cur.execute(query)
        yield from cur


def export():
    backup = WORK / 'snapshot'
    if backup.exists():
        raise RuntimeError('Snapshot directory already exists; refusing to overwrite')
    backup.mkdir()
    manifest = {'database': 'kite', 'started_at': dt.datetime.now(UTC).isoformat(), 'tables': {}}
    con = source()
    try:
        with con.cursor() as cur:
            cur.execute('SELECT table_name, engine, auto_increment FROM information_schema.tables WHERE table_schema=%s AND table_type=%s ORDER BY table_name', ('kite', 'BASE TABLE'))
            tables = cur.fetchall()
            if sorted(t for t, _, _ in tables) != EXPECTED_TABLES or any(engine != 'InnoDB' for _, engine, _ in tables):
                raise RuntimeError('Unexpected source tables/engines; manual review required')
            for table, _, next_id in tables:
                cur.execute('SELECT column_name FROM information_schema.columns WHERE table_schema=%s AND table_name=%s ORDER BY ordinal_position', ('kite', table))
                columns = [r[0] for r in cur.fetchall()]
                cur.execute('SELECT column_name FROM information_schema.statistics WHERE table_schema=%s AND table_name=%s AND index_name=%s ORDER BY seq_in_index', ('kite', table, 'PRIMARY'))
                pk = [r[0] for r in cur.fetchall()]
                if not pk:
                    raise RuntimeError('Source table without primary key: ' + table)
                manifest['tables'][table] = {'columns': columns, 'primary_key': pk, 'next_id': next_id}
        for table, info in manifest['tables'].items():
            emit('Reading MySQL table: ' + table)
            digest = hashlib.sha256()
            count = 0
            with gzip.open(backup / (table + '.jsonl.gz'), 'wb', compresslevel=1) as out:
                for row in rows(con, table, info):
                    record = line(row)
                    digest.update(record)
                    out.write(record)
                    count += 1
            info['rows'] = count
            info['raw_sha256'] = digest.hexdigest()
            emit(f'Backed up {table}: {count} rows')
        manifest['completed_at'] = dt.datetime.now(UTC).isoformat()
        (backup / 'manifest.json').write_text(json.dumps(manifest, indent=2), encoding='utf-8')
        emit('Read-only consistent snapshot complete')
    finally:
        con.rollback()
        con.close()


def pg_columns(cur):
    cur.execute("SELECT table_name,column_name,data_type,is_nullable FROM information_schema.columns WHERE table_schema='public' ORDER BY table_name,ordinal_position")
    result = {}
    for table, column, kind, nullable in cur.fetchall():
        result.setdefault(table, {})[column] = (kind, nullable == 'YES')
    return result


def convert(value, kind, nullable, context):
    if value is None:
        if not nullable:
            raise ValueError(context + ': NULL in non-nullable target column')
        return None
    if kind == 'boolean':
        if value not in (0, 1, False, True):
            raise ValueError(context + ': non-boolean source value')
        return bool(value)
    if kind in ('text', 'character varying', 'character'):
        if isinstance(value, bytes):
            value = value.decode('utf-8', errors='strict')
        if not isinstance(value, str) or '\x00' in value:
            raise ValueError(context + ': unsupported text value')
        return value
    if kind.startswith('timestamp'):
        # Zero dates must be reviewed rather than silently changed to NULL.
        if not isinstance(value, dt.datetime):
            raise ValueError(context + ': invalid source timestamp; review zero dates')
        if kind == 'timestamp with time zone':
            if value.tzinfo is None:
                value = value.replace(tzinfo=SHANGHAI)
            return value.astimezone(UTC)
        return value.replace(tzinfo=None)
    if kind in ('bigint', 'integer', 'smallint'):
        if not isinstance(value, int):
            raise ValueError(context + ': non-integer source value')
        return value
    raise ValueError(context + ': unreviewed PostgreSQL type ' + kind)


def converted(row, columns, types, table):
    return [convert(v, *types[c], table + '.' + c) for c, v in zip(columns, row, strict=True)]


def order_tables(cur, names):
    cur.execute("SELECT child.relname,parent.relname FROM pg_constraint c JOIN pg_class child ON child.oid=c.conrelid JOIN pg_class parent ON parent.oid=c.confrelid WHERE c.contype='f' AND c.connamespace='public'::regnamespace")
    dependencies = {name: set() for name in names}
    for child, parent in cur.fetchall():
        if child in dependencies:
            if parent not in dependencies:
                raise RuntimeError('Foreign key outside selected migration tables')
            dependencies[child].add(parent)
    ordered = []
    while dependencies:
        ready = sorted(n for n, deps in dependencies.items() if not deps)
        if not ready:
            raise RuntimeError('Cyclic foreign keys; manual review required')
        for name in ready:
            ordered.append(name)
            del dependencies[name]
        for deps in dependencies.values():
            deps.difference_update(ready)
    return ordered


def backup_target(con, cur, types, source_manifest):
    folder = WORK / ('pg-before-overwrite-' + dt.datetime.now(UTC).strftime('%H%M%S') + '-' + __import__('uuid').uuid4().hex[:6])
    folder.mkdir()
    manifest = {'database':'kite', 'started_at':dt.datetime.now(UTC).isoformat(), 'tables':{}}
    for table in sorted(types):
        emit('Backing up PG table: ' + table)
        columns = list(types[table])
        primary_key = source_manifest['tables'][table]['primary_key']
        digest = hashlib.sha256()
        count = 0
        with gzip.open(folder / (table + '.jsonl.gz'), 'wb', compresslevel=1) as out, con.cursor(name='backup_' + table) as reader:
            reader.execute(sql.SQL('SELECT {} FROM {} ORDER BY {}').format(sql.SQL(',').join(map(sql.Identifier, columns)), sql.Identifier('public',table),sql.SQL(',').join(map(sql.Identifier,primary_key))))
            while batch := reader.fetchmany(100):
                for row in batch:
                    record = line(row)
                    out.write(record)
                    digest.update(record)
                    count += 1
        manifest['tables'][table] = {'columns':columns,'primary_key':primary_key,'rows':count,'raw_sha256':digest.hexdigest()}
        emit(f'Backed up existing PG {table}: {count} rows')
    cur.execute("SELECT schemaname,sequencename,last_value,start_value,increment_by FROM pg_sequences WHERE schemaname='public'")
    manifest['sequences'] = [list(row) for row in cur.fetchall()]
    cur.execute("SELECT child.relname,c.conname,pg_get_constraintdef(c.oid),c.convalidated FROM pg_constraint c JOIN pg_class child ON child.oid=c.conrelid WHERE c.connamespace='public'::regnamespace")
    manifest['constraints'] = [list(row) for row in cur.fetchall()]
    manifest['completed_at'] = dt.datetime.now(UTC).isoformat()
    (folder/'manifest.json').write_text(json.dumps(manifest,indent=2),encoding='utf-8')
    emit('Existing PG data backup saved: ' + str(folder))


def connection_params():
    return dict(CONFIG['postgres'], keepalives=1, keepalives_idle=30,
                keepalives_interval=10, keepalives_count=6, tcp_user_timeout=600000,
                application_name='kite_mysql_pg_migration')


def validate_snapshot(folder):
    manifest = json.loads((folder / 'manifest.json').read_text(encoding='utf-8'))
    if manifest.get('database') != 'kite' or sorted(manifest.get('tables', {})) != EXPECTED_TABLES or not manifest.get('completed_at'):
        raise RuntimeError('Incomplete or unexpected Kite snapshot')
    for table, info in manifest['tables'].items():
        if not info['primary_key'] or not set(info['primary_key']).issubset(info['columns']):
            raise RuntimeError('Invalid snapshot primary key: ' + table)
        digest = hashlib.sha256()
        count = 0
        with gzip.open(folder / (table + '.jsonl.gz'), 'rb') as inp:
            for record in inp:
                digest.update(record)
                count += 1
        if count != info['rows'] or digest.hexdigest() != info['raw_sha256']:
            raise RuntimeError('Snapshot checksum/count mismatch: ' + table)
        emit(f'Snapshot verified {table}: {count} rows')
    return manifest


def copy_batches(cur, statement, inp, columns, types, table, total, raw_digest, expected):
    # Bound each COPY command by row count and raw size; all batches share the same transaction.
    count = 0
    last_progress = time.monotonic()
    while True:
        batch = []
        size = 0
        for record in itertools.islice(inp, 100):
            raw_digest.update(record)
            values = converted(unpacked(json.loads(record)), columns, types[table], table)
            expected.update(line(values))
            batch.append(values)
            size += len(record)
            if size >= 4 * 1024 * 1024:
                break
        if not batch:
            return count
        with cur.copy(statement) as copy:
            for values in batch:
                copy.write_row(values)
        count += len(batch)
        if time.monotonic() - last_progress >= 5 or count == total:
            emit(f'COPY {table}: {count}/{total} rows (not committed)')
            last_progress = time.monotonic()


def load_with_retry(retries):
    for attempt in range(1, retries + 1):
        try:
            load()
            return
        except psycopg.OperationalError:
            if COMMIT_STARTED or attempt == retries:
                raise
            emit(f'Connection interrupted during {PHASE}; transaction did not reach COMMIT. Retrying full import {attempt + 1}/{retries} from saved snapshot.')
            time.sleep(min(3 * attempt, 10))


def ensure_database():
    with psycopg.connect(**dict(connection_params(),dbname='postgres'), autocommit=True) as con:
        with con.cursor() as cur:
            cur.execute('SELECT 1 FROM pg_database WHERE datname=%s', ('kite',))
            if cur.fetchone() is None:
                cur.execute(sql.SQL('CREATE DATABASE {}').format(sql.Identifier('kite')))
                emit('Created new target database kite')


def load():
    global COMMIT_STARTED, PHASE
    COMMIT_STARTED = False
    PHASE = 'connecting'
    backup = SNAPSHOT or WORK / 'snapshot'
    manifest = json.loads((backup / 'manifest.json').read_text())
    pg = dict(connection_params(), dbname='kite')
    report = {'snapshot_started_at': manifest['started_at'], 'tables': {}}
    with psycopg.connect(**pg) as con:
        with con.cursor() as cur:
            cur.execute("SET LOCAL TIME ZONE 'Asia/Shanghai'")
            cur.execute("SET LOCAL statement_timeout = 0")
            cur.execute("SET LOCAL idle_in_transaction_session_timeout = 0")
            cur.execute("SET LOCAL transaction_timeout = 0")
            cur.execute('SELECT current_database(),oid FROM pg_database WHERE datname=current_database()')
            if cur.fetchone()[0] != 'kite':
                raise RuntimeError('Target identity mismatch')
            cur.execute("SET LOCAL lock_timeout = '10s'")
            cur.execute("SELECT pg_advisory_xact_lock(13783712)")
            cur.execute("SELECT count(*) FROM pg_trigger WHERE NOT tgisinternal AND tgrelid IN (SELECT oid FROM pg_class WHERE relnamespace='public'::regnamespace)")
            if cur.fetchone()[0]:
                raise RuntimeError('Unexpected user triggers; refusing import')
            types = pg_columns(cur)
            if not types:
                cur.execute((ROOT / 'schema.sql').read_text(encoding='utf-8'), prepare=False)
                cur.execute("SET LOCAL TIME ZONE 'Asia/Shanghai'")
                types = pg_columns(cur)
                cur.execute("SET LOCAL statement_timeout = 0")
                cur.execute("SET LOCAL idle_in_transaction_session_timeout = 0")
                cur.execute("SET LOCAL transaction_timeout = 0")
                cur.execute("SET LOCAL lock_timeout = '10s'")
            if set(types) != set(manifest['tables']):
                raise RuntimeError('Source and target table lists differ')
            ordered = order_tables(cur, manifest['tables'])
            # Lock and validate EVERY target table before writing any rows.
            cur.execute(sql.SQL('LOCK TABLE {} IN ACCESS EXCLUSIVE MODE').format(sql.SQL(',').join(sql.Identifier('public', n) for n in sorted(types))))
            for table, info in manifest['tables'].items():
                if set(info['columns']) != set(types[table]):
                    raise RuntimeError('Column mismatch for ' + table)
            legacy_fks = {
                'fk_resource_histories_operator': {'child':'resource_histories','column':'operator_id','parent':'users'},
                'fk_roles_assignments': {'child':'role_assignments','column':'role_id','parent':'roles'},
                'fk_user_group_members_user': {'child':'user_group_members','column':'user_id','parent':'users'},
                'fk_user_group_members_user_group': {'child':'user_group_members','column':'user_group_id','parent':'user_groups'},
            }
            # Refuse dependencies from unrelated schemas/tables; never use CASCADE.
            cur.execute("SELECT c.conname FROM pg_constraint c JOIN pg_class child ON child.oid=c.conrelid JOIN pg_class parent ON parent.oid=c.confrelid WHERE c.contype='f' AND parent.relnamespace='public'::regnamespace AND child.relnamespace<>'public'::regnamespace")
            if cur.fetchone():
                raise RuntimeError('External foreign keys reference Kite tables; refusing overwrite')
            PHASE = 'backing up PG'
            backup_target(con, cur, types, manifest)

            cur.execute("SELECT child.relname,c.conname,pg_get_constraintdef(c.oid) FROM pg_constraint c JOIN pg_class child ON child.oid=c.conrelid WHERE c.contype='f' AND c.connamespace='public'::regnamespace")
            saved_fks = cur.fetchall()
            if {name for table, name, definition in saved_fks} != set(legacy_fks):
                raise RuntimeError('Unexpected foreign key definitions')
            for table, name, definition in saved_fks:
                info = legacy_fks[name]
                expected_definition = 'FOREIGN KEY (' + info['column'] + ') REFERENCES users(id)' if info['parent'] == 'users' else 'FOREIGN KEY (' + info['column'] + ') REFERENCES ' + info['parent'] + '(id)'
                # pg_get_constraintdef can qualify tables depending on search_path.
                if table != info['child'] or expected_definition not in definition.replace('public.', ''):
                    raise RuntimeError('Unexpected foreign key mapping')
                cur.execute(sql.SQL('ALTER TABLE {} DROP CONSTRAINT {}').format(sql.Identifier('public', table), sql.Identifier(name)))
            for table in reversed(ordered):
                cur.execute(sql.SQL('DELETE FROM {}').format(sql.Identifier('public', table)))
            emit('Target Kite data cleared inside the transaction; importing ALL source rows')
            for table in ordered:
                PHASE = 'copying ' + table
                emit('Importing MySQL table: ' + table)
                info = manifest['tables'][table]
                columns = info['columns']
                raw_digest = hashlib.sha256()
                expected = hashlib.sha256()
                count = 0
                statement = sql.SQL('COPY {} ({}) FROM STDIN').format(sql.Identifier('public', table), sql.SQL(',').join(map(sql.Identifier, columns)))
                with gzip.open(backup / (table + '.jsonl.gz'), 'rb') as inp:
                    count = copy_batches(cur, statement, inp, columns, types, table, info['rows'], raw_digest, expected)
                if count != info['rows'] or raw_digest.hexdigest() != info['raw_sha256']:
                    raise RuntimeError('Snapshot checksum/count mismatch: ' + table)
                PHASE = 'verifying ' + table
                emit('Verifying all fields: ' + table)
                # Check all columns, including encrypted credentials, JSON/YAML and IDs.
                actual = hashlib.sha256()
                fetched = 0
                with con.cursor(name='verify_' + table) as reader:
                    reader.execute(sql.SQL('SELECT {} FROM {} ORDER BY {}').format(sql.SQL(',').join(map(sql.Identifier, columns)), sql.Identifier('public', table), sql.SQL(',').join(map(sql.Identifier, info['primary_key']))))
                    while batch := reader.fetchmany(100):
                        for row in batch:
                            actual.update(line(converted(row, columns, types[table], table)))
                            fetched += 1
                if fetched != count or actual.digest() != expected.digest():
                    raise RuntimeError('Target full-content verification failed: ' + table)
                report['tables'][table] = {'rows': count, 'sha256': actual.hexdigest(), 'verified': True}
                emit(f'Copied and verified {table}: {count} rows')
            report['legacy_foreign_keys'] = {}
            unvalidated = 0
            for table, name, definition in saved_fks:
                info = legacy_fks[name]
                cur.execute(sql.SQL('SELECT child.{column},count(*) FROM {child} child LEFT JOIN {parent} parent ON parent.id=child.{column} WHERE child.{column} IS NOT NULL AND parent.id IS NULL GROUP BY child.{column}').format(column=sql.Identifier(info['column']), child=sql.Identifier('public',table), parent=sql.Identifier('public',info['parent'])))
                orphans = {str(value): count for value, count in cur.fetchall()}
                definition = definition.removesuffix(' NOT VALID')
                cur.execute(sql.SQL('ALTER TABLE {} ADD CONSTRAINT {} {} NOT VALID').format(sql.Identifier('public',table), sql.Identifier(name), sql.SQL(definition)))
                if not orphans:
                    cur.execute(sql.SQL('ALTER TABLE {} VALIDATE CONSTRAINT {}').format(sql.Identifier('public',table), sql.Identifier(name)))
                else:
                    unvalidated += 1
                    emit('Preserved legacy foreign key exceptions: ' + name + ': ' + str(sum(orphans.values())) + ' rows; NOT VALID for historical rows')
                report['legacy_foreign_keys'][name] = {'orphan_ids': orphans, 'validated': not bool(orphans)}
            # Preserve the source high-water mark, including previously deleted IDs.
            for table, info in manifest['tables'].items():
                if info['next_id'] is None:
                    continue
                cur.execute('SELECT pg_get_serial_sequence(%s,%s)', ('public.' + table, 'id'))
                sequence = cur.fetchone()[0]
                if sequence is None:
                    raise RuntimeError('Missing ID sequence for ' + table)
                cur.execute(sql.SQL('SELECT coalesce(max(id),0) FROM {}').format(sql.Identifier('public', table)))
                maximum = max(cur.fetchone()[0], info['next_id'] - 1)
                # ALTER SEQUENCE RESTART is transactional, unlike setval.
                cur.execute(sql.SQL('ALTER SEQUENCE {} RESTART WITH {}').format(sql.Identifier(*sequence.split('.')), sql.Literal(max(1, maximum + 1))))
            cur.execute("SELECT count(*) FROM pg_constraint WHERE connamespace='public'::regnamespace AND contype='f' AND NOT convalidated")
            if cur.fetchone()[0] != unvalidated:
                raise RuntimeError('Unexpected unvalidated foreign keys')
        PHASE = 'committing'
        COMMIT_STARTED = True
        con.commit()
        PHASE = 'committed'
    report['committed_at'] = dt.datetime.now(UTC).isoformat()
    (WORK / 'migration-report.json').write_text(json.dumps(report, indent=2), encoding='utf-8')
    emit('SUCCESS: all MySQL tables and data verified and committed into PostgreSQL kite')



def main():
    global WORK, SNAPSHOT
    parser = argparse.ArgumentParser(description='Read ALL MySQL Kite data and overwrite PostgreSQL Kite tables')
    parser.add_argument('--mysql-host', default=os.getenv('MYSQL_HOST','127.0.0.1'))
    parser.add_argument('--mysql-port', type=int, default=int(os.getenv('MYSQL_PORT','12346')))
    parser.add_argument('--mysql-user', default=os.getenv('MYSQL_USER','root'))
    parser.add_argument('--pg-host', default=os.getenv('PGHOST','pgm-uf6ms6ff0v0ptdun1o.rwlb.rds.aliyuncs.com'))
    parser.add_argument('--pg-port', type=int, default=int(os.getenv('PGPORT','5432')))
    parser.add_argument('--pg-user', default=os.getenv('PGUSER','magik'))
    parser.add_argument('--snapshot', type=pathlib.Path, help='Reuse an existing completed snapshot or run directory; do not connect to MySQL')
    parser.add_argument('--retries', type=int, default=5, help='Maximum reconnect attempts for one resumable batch (default: 5)')
    parser.add_argument('--stage-only', action='store_true', help='Upload and verify staging data, without replacing public tables')
    args=parser.parse_args()
    if args.retries < 1:
        parser.error('--retries must be positive')
    if args.snapshot:
        SNAPSHOT = args.snapshot.resolve()
        if not (SNAPSHOT / 'manifest.json').is_file():
            SNAPSHOT = SNAPSHOT / 'snapshot'
        if not (SNAPSHOT / 'manifest.json').is_file():
            parser.error('No completed snapshot manifest found')
    mysql_password='' if SNAPSHOT else os.getenv('MYSQL_PASSWORD') or getpass.getpass('MySQL password: ')
    pg_password=os.getenv('PGPASSWORD') or getpass.getpass('PostgreSQL password: ')
    CONFIG['mysql']=dict(host=args.mysql_host,port=args.mysql_port,user=args.mysql_user,password=mysql_password,database='kite',charset='utf8mb4',connect_timeout=15)
    CONFIG['postgres']=dict(host=args.pg_host,port=args.pg_port,user=args.pg_user,password=pg_password,dbname='kite',connect_timeout=15,sslmode=os.getenv('PGSSLMODE','prefer'))
    os.umask(0o077)
    WORK=ROOT/'runs'/(dt.datetime.now(UTC).strftime('%Y%m%dT%H%M%S')+'-'+__import__('uuid').uuid4().hex[:8])
    WORK.mkdir(parents=True,exist_ok=False)
    emit('SOURCE snapshot: ' + str(SNAPSHOT) if SNAPSHOT else f'SOURCE read-only: {args.mysql_host}:{args.mysql_port}/kite')
    emit(f'TARGET OVERWRITE: {args.pg_host}:{args.pg_port}/kite (16 Kite tables, ALL rows)')
    emit('Artifacts: '+str(WORK))
    try:
        if not SNAPSHOT:
            export()
        validate_snapshot(SNAPSHOT or WORK / 'snapshot')
        ensure_database()
        import resumable
        resumable.run(sys.modules[__name__], SNAPSHOT or WORK / 'snapshot', retries=args.retries, stage_only=args.stage_only)
        return 0
    except Exception as exc:
        message=str(exc)
        for secret in (mysql_password,pg_password):
            if secret:
                message=message.replace(secret,'[REDACTED]')
        print(type(exc).__name__+': '+message,file=sys.stderr)
        print('Failure phase: ' + PHASE, file=sys.stderr)
        print('Verified batches are preserved in the dedicated PG migration schema. Rerun with the SAME --snapshot to resume; publication state is checked before any repeated switch.', file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())

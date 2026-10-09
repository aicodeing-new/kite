"""Checkpointed uploads on short connections, followed by one atomic schema switch."""
import datetime as dt
import gzip
import hashlib
import itertools
import json
import time
import uuid

import psycopg
from psycopg import sql

LOCK = 13783712


def connect(m):
    return psycopg.connect(**m.connection_params())


def columns(cur, schema):
    cur.execute("SELECT table_name,column_name,data_type,is_nullable FROM information_schema.columns WHERE table_schema=%s ORDER BY table_name,ordinal_position", (schema,))
    result = {}
    for table, column, kind, nullable in cur.fetchall():
        result.setdefault(table, {})[column] = (kind, nullable == 'YES')
    return result


def state(cur, schema):
    cur.execute(sql.SQL('SELECT snapshot_hash,phase,report,fks FROM {} WHERE id=1').format(sql.Identifier(schema,'_migration_state')))
    return cur.fetchone()


def setup(m, schema, fingerprint, manifest):
    with connect(m) as con, con.cursor() as cur:
        cur.execute('SELECT pg_advisory_xact_lock(%s)', (LOCK,))
        cur.execute('SELECT to_regnamespace(%s)', (schema,))
        if cur.fetchone()[0] is not None:
            found = state(cur, schema)
            if found[0] != fingerprint:
                raise RuntimeError('Staging identity mismatch')
            return found
        target = columns(cur,'public')
        if target and set(target) != set(manifest['tables']):
            raise RuntimeError('Unexpected public tables; refusing migration')
        for table, info in manifest['tables'].items():
            if target and set(target[table]) != set(info['columns']):
                raise RuntimeError('Target column mismatch: '+table)
        cur.execute(sql.SQL('CREATE SCHEMA {}').format(sql.Identifier(schema)))
        ddl = (m.ROOT/'schema.sql').read_text(encoding='utf-8').replace('public.',schema+'.')
        cur.execute(ddl,prepare=False)
        cur.execute(sql.SQL('CREATE TABLE {} (id integer PRIMARY KEY CHECK(id=1), snapshot_hash text NOT NULL, phase text NOT NULL, report text NOT NULL, fks text NOT NULL)').format(sql.Identifier(schema,'_migration_state')))
        cur.execute(sql.SQL('CREATE TABLE {} (table_name text NOT NULL, batch_no bigint NOT NULL, row_count bigint NOT NULL, raw_hash text NOT NULL, content_hash text NOT NULL, PRIMARY KEY(table_name,batch_no))').format(sql.Identifier(schema,'_migration_batches')))
        staged = columns(cur,schema)
        for table, info in manifest['tables'].items():
            if set(staged[table]) != set(info['columns']):
                raise RuntimeError('Snapshot column mismatch: '+table)
        cur.execute("SELECT child.relname,c.conname,pg_get_constraintdef(c.oid) FROM pg_constraint c JOIN pg_class child ON child.oid=c.conrelid WHERE c.contype='f' AND c.connamespace=to_regnamespace(%s)", (schema,))
        fks = cur.fetchall()
        if len(fks) != 4:
            raise RuntimeError('Unexpected staging foreign keys')
        for table, name, definition in fks:
            cur.execute(sql.SQL('ALTER TABLE {} DROP CONSTRAINT {}').format(sql.Identifier(schema,table),sql.Identifier(name)))
        cur.execute(sql.SQL('INSERT INTO {} VALUES (1,%s,%s,%s,%s)').format(sql.Identifier(schema,'_migration_state')), (fingerprint,'uploading','{}',json.dumps(fks)))
        return fingerprint,'uploading','{}',json.dumps(fks)


def send_batch(m, schema, table, info, batch_no, records, types, retries, compression_schema=None):
    raw = hashlib.sha256(b''.join(records)).hexdigest()
    values = [m.converted(m.unpacked(json.loads(record)),info['columns'],types,table) for record in records]
    expected = hashlib.sha256(b''.join(m.line(row) for row in values)).hexdigest()
    for attempt in range(retries):
        try:
            # Each connection handles one bounded batch only. Checkpoint and data commit together.
            with connect(m) as con, con.cursor() as cur:
                cur.execute(sql.SQL('SELECT phase FROM {} WHERE id=1 FOR UPDATE').format(sql.Identifier(schema,'_migration_state')))
                if cur.fetchone()[0] != 'uploading':
                    raise RuntimeError('Migration is no longer accepting uploads')
                cur.execute(sql.SQL('SELECT row_count,raw_hash,content_hash FROM {} WHERE table_name=%s AND batch_no=%s').format(sql.Identifier(schema,'_migration_batches')),(table,batch_no))
                prior = cur.fetchone()
                if prior:
                    if prior != (len(records),raw,expected):
                        raise RuntimeError('Checkpoint mismatch: '+table)
                    return raw,expected
                if compression_schema:
                    row_sql = []
                    arguments = []
                    for row in values:
                        expressions = []
                        for value in row:
                            if isinstance(value,str) and len(value) > 4096:
                                encoded = value.encode('utf-8')
                                pieces = []
                                # The RDS gzip 1.0 build cannot inflate large outputs in one call.
                                # Concatenate small bytea outputs BEFORE UTF-8 decoding, so boundaries are lossless.
                                for offset in range(0,len(encoded),65536):
                                    pieces.append(sql.SQL('{}.gunzip(%s::bytea)').format(sql.Identifier(compression_schema)))
                                    arguments.append(gzip.compress(encoded[offset:offset+65536],compresslevel=1,mtime=0))
                                expressions.append(sql.SQL("convert_from(({}),'UTF8')").format(sql.SQL(' || ').join(pieces)))
                            else:
                                expressions.append(sql.Placeholder())
                                arguments.append(value)
                        row_sql.append(sql.SQL('({})').format(sql.SQL(',').join(expressions)))
                    cur.execute(sql.SQL('INSERT INTO {} ({}) VALUES {}').format(sql.Identifier(schema,table),sql.SQL(',').join(map(sql.Identifier,info['columns'])),sql.SQL(',').join(row_sql)),arguments,prepare=False)
                else:
                    statement = sql.SQL('COPY {} ({}) FROM STDIN').format(sql.Identifier(schema,table),sql.SQL(',').join(map(sql.Identifier,info['columns'])))
                    with cur.copy(statement) as copy:
                        for row in values:
                            copy.write_row(row)
                pk = info['primary_key']
                indices = [info['columns'].index(key) for key in pk]
                lower = [values[0][i] for i in indices]
                upper = [values[-1][i] for i in indices]
                key_sql = sql.SQL(',').join(map(sql.Identifier,pk))
                placeholders = sql.SQL(',').join(sql.Placeholder() for _ in pk)
                projection = [sql.SQL("encode(sha256(convert_to({},'UTF8')),'hex')").format(sql.Identifier(column)) if types[column][0] in ('text','character varying','character') else sql.Identifier(column) for column in info['columns']]
                cur.execute(sql.SQL('SELECT {} FROM {} WHERE ({}) >= ({}) AND ({}) <= ({}) ORDER BY {}').format(sql.SQL(',').join(projection),sql.Identifier(schema,table),key_sql,placeholders,key_sql,placeholders,key_sql),lower+upper)
                rows = cur.fetchall()
                actual = hashlib.sha256(b''.join(m.line(m.converted(row,info['columns'],types,table)) for row in rows)).hexdigest()
                verify_values = [[hashlib.sha256(value.encode('utf-8')).hexdigest() if value is not None and types[column][0] in ('text','character varying','character') else value for column,value in zip(info['columns'],row,strict=True)] for row in values]
                expected_verification = hashlib.sha256(b''.join(m.line(row) for row in verify_values)).hexdigest()
                if len(rows) != len(values) or actual != expected_verification:
                    raise RuntimeError('Staging full-field verification failed: '+table)
                cur.execute(sql.SQL('INSERT INTO {} VALUES (%s,%s,%s,%s,%s)').format(sql.Identifier(schema,'_migration_batches')),(table,batch_no,len(records),raw,expected))
            return raw,expected
        except psycopg.OperationalError as exc:
            if attempt+1 == retries:
                raise
            m.emit(f'Batch connection interrupted: {table} batch {batch_no}; reconnecting and checking checkpoint {attempt+2}/{retries}')
            time.sleep(1)


def upload(m, schema, manifest, folder, retries):
    with connect(m) as con, con.cursor() as cur:
        types = columns(cur,schema)
        cur.execute("SELECT n.nspname FROM pg_extension e JOIN pg_namespace n ON n.oid=e.extnamespace WHERE e.extname='gzip'")
        compression = cur.fetchone()
        compression_schema = compression[0] if compression else None
        if compression_schema:
            m.emit('Compressed upload enabled; large text is decompressed on PG and every field verified by SHA-256')
        cur.execute(sql.SQL('SELECT table_name,batch_no,row_count,raw_hash,content_hash FROM {}').format(sql.Identifier(schema,'_migration_batches')))
        checkpoints = {(table,number):(count,raw,content) for table,number,count,raw,content in cur.fetchall()}
    report = {'snapshot_started_at':manifest['started_at'],'staging_schema':schema,'tables':{}}
    last = time.monotonic()
    for table, info in manifest['tables'].items():
        m.PHASE = 'staging ' + table
        number = count = 0
        raw_digest = hashlib.sha256()
        content_digest = hashlib.sha256()
        m.emit('Uploading with durable checkpoints: '+table)
        with gzip.open(folder/(table+'.jsonl.gz'),'rb') as inp:
            while True:
                records = []
                size = 0
                for record in itertools.islice(inp,100):
                    records.append(record)
                    size += len(record)
                    if size >= 1024*1024:
                        break
                if not records:
                    break
                expected_raw = hashlib.sha256(b''.join(records)).hexdigest()
                normalized = [m.line(m.converted(m.unpacked(json.loads(record)),info['columns'],types[table],table)) for record in records]
                expected_content = hashlib.sha256(b''.join(normalized)).hexdigest()
                prior = checkpoints.get((table,number))
                if prior:
                    if prior != (len(records),expected_raw,expected_content):
                        raise RuntimeError('Existing checkpoint mismatch: '+table)
                else:
                    send_batch(m,schema,table,info,number,records,types[table],retries,compression_schema)
                for record, value in zip(records,normalized,strict=True):
                    raw_digest.update(record)
                    content_digest.update(value)
                count += len(records)
                number += 1
                if time.monotonic()-last >= 5 or count == info['rows']:
                    m.emit(f'STAGED AND VERIFIED {table}: {count}/{info["rows"]} rows (resumable; public unchanged)')
                    last = time.monotonic()
        if count != info['rows'] or raw_digest.hexdigest() != info['raw_sha256']:
            raise RuntimeError('Snapshot hash/count mismatch: '+table)
        report['tables'][table] = {'rows':count,'sha256':content_digest.hexdigest(),'verified':True,'batches':number}
    return report


def prepare(m, schema, manifest, report):
    m.PHASE = 'validating staged tables and constraints'
    with connect(m) as con, con.cursor() as cur:
        cur.execute(sql.SQL('SELECT fks,phase FROM {} WHERE id=1 FOR UPDATE').format(sql.Identifier(schema,'_migration_state')))
        fks,phase = cur.fetchone()
        if phase != 'uploading':
            return
        for table, info in manifest['tables'].items():
            cur.execute(sql.SQL('SELECT count(*) FROM {}').format(sql.Identifier(schema,table)))
            if cur.fetchone()[0] != info['rows']:
                raise RuntimeError('Final staged table count mismatch: '+table)
            cur.execute(sql.SQL('SELECT coalesce(sum(row_count),0),count(*) FROM {} WHERE table_name=%s').format(sql.Identifier(schema,'_migration_batches')),(table,))
            row_count,batches = cur.fetchone()
            if row_count != info['rows'] or batches != report['tables'][table]['batches']:
                raise RuntimeError('Unexpected extra or missing upload batches: '+table)
            if info['next_id'] is not None:
                cur.execute('SELECT pg_get_serial_sequence(%s,%s)',(schema+'.'+table,'id'))
                sequence = cur.fetchone()[0]
                cur.execute(sql.SQL('SELECT coalesce(max(id),0) FROM {}').format(sql.Identifier(schema,table)))
                next_id = max(cur.fetchone()[0]+1,info['next_id'],1)
                cur.execute(sql.SQL('ALTER SEQUENCE {} RESTART WITH {}').format(sql.Identifier(*sequence.split('.')),sql.Literal(next_id)))
        report['legacy_foreign_keys'] = {}
        relationships = {'resource_histories':('operator_id','users'),'role_assignments':('role_id','roles')}
        for table,name,definition in json.loads(fks):
            cur.execute(sql.SQL('ALTER TABLE {} ADD CONSTRAINT {} {} NOT VALID').format(sql.Identifier(schema,table),sql.Identifier(name),sql.SQL(definition.removesuffix(' NOT VALID'))))
            if table in relationships:
                column,parent = relationships[table]
            elif name == 'fk_user_group_members_user':
                column,parent = 'user_id','users'
            elif name == 'fk_user_group_members_user_group':
                column,parent = 'user_group_id','user_groups'
            else:
                raise RuntimeError('Unexpected foreign key')
            cur.execute(sql.SQL('SELECT child.{column},count(*) FROM {child} child LEFT JOIN {parent} parent ON parent.id=child.{column} WHERE child.{column} IS NOT NULL AND parent.id IS NULL GROUP BY child.{column}').format(column=sql.Identifier(column),child=sql.Identifier(schema,table),parent=sql.Identifier(schema,parent)))
            orphans = {str(value):count for value,count in cur.fetchall()}
            if not orphans:
                cur.execute(sql.SQL('ALTER TABLE {} VALIDATE CONSTRAINT {}').format(sql.Identifier(schema,table),sql.Identifier(name)))
            report['legacy_foreign_keys'][name] = {'orphan_ids':orphans,'validated':not bool(orphans)}
        cur.execute(sql.SQL('UPDATE {} SET phase=%s,report=%s WHERE id=1').format(sql.Identifier(schema,'_migration_state')),('ready',json.dumps(report)))


def publish(m, schema, manifest):
    m.PHASE = 'atomically publishing staged tables'
    with connect(m) as con, con.cursor() as cur:
        cur.execute("SET LOCAL lock_timeout='10s'")
        cur.execute('SELECT pg_advisory_xact_lock(%s)',(LOCK,))
        cur.execute(sql.SQL('SELECT phase,report FROM {} WHERE id=1 FOR UPDATE').format(sql.Identifier(schema,'_migration_state')))
        phase,data = cur.fetchone()
        report = json.loads(data)
        if phase == 'published':
            return report
        if phase != 'ready':
            raise RuntimeError('Staged upload is not complete')
        target = columns(cur,'public')
        if target:
            if set(target) != set(manifest['tables']):
                raise RuntimeError('Unexpected public objects; refusing switch')
            cur.execute(sql.SQL('LOCK TABLE {} IN ACCESS EXCLUSIVE MODE').format(sql.SQL(',').join(sql.Identifier('public',t) for t in sorted(target))))
            for table,info in manifest['tables'].items():
                if set(target[table]) != set(info['columns']):
                    raise RuntimeError('Target column mismatch: '+table)
            cur.execute("SELECT count(*) FROM pg_trigger WHERE NOT tgisinternal AND tgrelid IN (SELECT oid FROM pg_class WHERE relnamespace='public'::regnamespace)")
            if cur.fetchone()[0]:
                raise RuntimeError('Unexpected target triggers')
            cur.execute("SELECT count(*) FROM pg_constraint c JOIN pg_class parent ON parent.oid=c.confrelid JOIN pg_class child ON child.oid=c.conrelid WHERE c.contype='f' AND parent.relnamespace='public'::regnamespace AND child.relnamespace<>'public'::regnamespace")
            if cur.fetchone()[0]:
                raise RuntimeError('External foreign keys reference target tables')
            cur.execute("SELECT count(*) FROM pg_depend d JOIN pg_rewrite r ON r.oid=d.objid JOIN pg_class parent ON parent.oid=d.refobjid WHERE d.classid='pg_rewrite'::regclass AND parent.relnamespace='public'::regnamespace")
            if cur.fetchone()[0]:
                raise RuntimeError('Views reference target tables; manual review required')
            backup = 'kite_before_'+dt.datetime.now(dt.timezone.utc).strftime('%Y%m%d_%H%M%S')+'_'+uuid.uuid4().hex[:6]
            cur.execute(sql.SQL('CREATE SCHEMA {}').format(sql.Identifier(backup)))
            for table in sorted(target):
                cur.execute(sql.SQL('ALTER TABLE {} SET SCHEMA {}').format(sql.Identifier('public',table),sql.Identifier(backup)))
            report['previous_pg_schema'] = backup
        for table in sorted(manifest['tables']):
            cur.execute(sql.SQL('ALTER TABLE {} SET SCHEMA public').format(sql.Identifier(schema,table)))
        report['committed_at'] = dt.datetime.now(dt.timezone.utc).isoformat()
        cur.execute(sql.SQL('UPDATE {} SET phase=%s,report=%s WHERE id=1').format(sql.Identifier(schema,'_migration_state')),('published',json.dumps(report)))
    return report


def run(m, folder, retries=5, stage_only=False):
    m.PHASE = 'preparing resumable migration'
    manifest = json.loads((folder/'manifest.json').read_text(encoding='utf-8'))
    fingerprint = hashlib.sha256((folder/'manifest.json').read_bytes()).hexdigest()
    schema = 'kite_migrate_'+fingerprint[:20]
    identity,phase,data,fks = setup(m,schema,fingerprint,manifest)
    if phase == 'published':
        report = json.loads(data)
        m.emit('This exact snapshot is already published; no target data changed')
    else:
        if phase == 'uploading':
            report = upload(m,schema,manifest,folder,retries)
            prepare(m,schema,manifest,report)
        if stage_only:
            m.emit('STAGING COMPLETE; public tables unchanged; schema='+schema)
            return schema
        # Retry publication only after checking its durable state, including uncertain COMMIT.
        for attempt in range(retries):
            try:
                report = publish(m,schema,manifest)
                break
            except psycopg.OperationalError:
                if attempt+1 == retries:
                    raise
                time.sleep(1)
    (m.WORK/'migration-report.json').write_text(json.dumps(report,indent=2),encoding='utf-8')
    m.PHASE = 'published'
    m.emit('SUCCESS: all tables and all data verified and atomically published to PostgreSQL kite')
    m.emit('Previous PostgreSQL tables preserved in schema: '+report.get('previous_pg_schema','(target was empty)'))
    return report

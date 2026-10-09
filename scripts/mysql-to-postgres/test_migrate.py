import hashlib
import io
import unittest
from unittest.mock import patch

import psycopg
import migrate


class FakeCopy:
    def __init__(self, owner):
        self.owner = owner
        self.rows = []

    def __enter__(self):
        return self

    def write_row(self, row):
        self.rows.append(row)

    def __exit__(self, *exc):
        self.owner.batches.append(self.rows)


class FakeCursor:
    def __init__(self):
        self.batches = []

    def copy(self, statement):
        return FakeCopy(self)


class MigrationTests(unittest.TestCase):
    def tearDown(self):
        migrate.COMMIT_STARTED = False

    def check_batches(self, text_size, count):
        values = [[i, 'x' * text_size] for i in range(count)]
        records = b''.join(migrate.line(row) for row in values)
        raw = hashlib.sha256()
        expected = hashlib.sha256()
        cur = FakeCursor()
        copied = migrate.copy_batches(cur, None, io.BytesIO(records), ['id','text'],
                                      {'fixture': {'id':('bigint',False),'text':('text',False)}},
                                      'fixture', count, raw, expected)
        self.assertEqual(copied, count)
        self.assertEqual([row for batch in cur.batches for row in batch], values)
        self.assertEqual(raw.hexdigest(), hashlib.sha256(records).hexdigest())
        self.assertEqual(expected.digest(), raw.digest())
        self.assertTrue(all(len(batch) <= 100 for batch in cur.batches))
        return cur.batches

    def test_row_limit_preserves_all_rows(self):
        self.assertEqual([len(batch) for batch in self.check_batches(10,251)], [100,100,51])

    def test_size_limit_preserves_all_rows(self):
        batches = self.check_batches(2*1024*1024,5)
        self.assertEqual([len(batch) for batch in batches], [2,2,1])

    def test_disconnect_before_commit_retries(self):
        migrate.COMMIT_STARTED = False
        with patch.object(migrate,'load',side_effect=[psycopg.OperationalError('lost'),None]) as load, patch.object(migrate.time,'sleep'):
            migrate.load_with_retry(3)
        self.assertEqual(load.call_count,2)

    def test_disconnect_during_commit_never_retries(self):
        def uncertain_commit():
            migrate.COMMIT_STARTED = True
            raise psycopg.OperationalError('lost during commit')
        with patch.object(migrate,'load',side_effect=uncertain_commit) as load, patch.object(migrate.time,'sleep'):
            with self.assertRaises(psycopg.OperationalError):
                migrate.load_with_retry(3)
        self.assertEqual(load.call_count,1)


if __name__ == '__main__':
    unittest.main()

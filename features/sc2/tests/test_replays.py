"""Replay retention must never remove unrelated files or publish partial saves."""
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch
sys.path.insert(0, str(Path(__file__).parents[1]))
import replays

class ReplayQueue(unittest.TestCase):
    def test_fifo_complete_bundles_and_failure_boundaries(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp)/'queue';data=Path(tmp)/'data';data.mkdir()
            game=Path(tmp)/'map';game.write_bytes(b'original map')
            with patch.dict(os.environ,SC2_REPLAY_DIR=str(root),XDG_DATA_HOME=str(data)):
                ids=[replays.save(b'replay '+bytes([i]),game,{'map':'test','result':'Victory'}) for i in range(11)]
                rows=replays.records(root)
                self.assertEqual([r['id'] for r in rows],ids[1:])
                self.assertFalse((root/ids[0]).exists())
                self.assertTrue(any((data/'Trash/files').iterdir()))
                self.assertEqual((replays.bundle(ids[-1])/'map.SC2Map').read_bytes(),b'original map')
                unrelated=root/'keep.txt';unrelated.write_text('keep')
                (root/('f'*32)).symlink_to(Path(tmp),target_is_directory=True)
                for invalid in ('../map','f'*32,ids[0]):
                    with self.assertRaises(ValueError):replays.bundle(invalid)
                with patch('replays.subprocess.run',side_effect=OSError('trash unavailable')):
                    with self.assertRaises(OSError):replays.save(b'new',game,{'map':'test','result':'Defeat'})
                self.assertEqual([r['id'] for r in replays.records(root)],ids[1:])
                self.assertEqual(unrelated.read_text(),'keep')
                self.assertEqual(game.read_bytes(),b'original map')
                self.assertFalse(list(root.glob('.pending-*')))

if __name__=='__main__':unittest.main()

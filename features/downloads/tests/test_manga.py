"""Incremental CBZ updates must preserve old pages and the original on failure."""
import importlib.util
import json
import os
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch
import zipfile

spec = importlib.util.spec_from_file_location("manga", Path(__file__).parents[1] / "scripts" / "manga.py")
manga = importlib.util.module_from_spec(spec)
spec.loader.exec_module(manga)


class Album(list):
    name = "Fixture"
    authors = ["Fixture author"]
    description = ""
    tags = []


class IncrementalSafety(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.dest = self.root / "library"
        self.dest.mkdir()
        self.book = self.dest / "Fixture.cbz"
        self.photos = [SimpleNamespace(photo_id=str(i), index=i, name=f"chapter{i}") for i in range(1, 4)]
        with zipfile.ZipFile(self.book, "w") as archive:
            archive.writestr("metadata.json", json.dumps({"id": "123", "local_chapters": 2}))
            archive.writestr("cover.jpg", b"original-cover")
            for photo in self.photos[:2]:
                archive.writestr(f"{manga.chapter_dirname(photo)}/001.jpg", f"original-{photo.photo_id}".encode())
        self.original = self.book.read_bytes()
        self.client = Mock()
        self.client.get_album_detail.return_value = Album(self.photos)
        self.client.download_album_cover.side_effect = RuntimeError("offline cover")
        for name in ("log", "progress"):
            mock = patch.object(manga, name)
            mock.start()
            self.addCleanup(mock.stop)

    def downloaded(self, photo_id, option):
        photo = next(p for p in self.photos if p.photo_id == photo_id)
        dest = Path(option) / manga.chapter_dirname(photo)
        dest.mkdir(parents=True, exist_ok=True)
        (dest / "001.jpg").write_bytes(f"downloaded-{photo_id}".encode())

    def move_old(self, path):
        with zipfile.ZipFile(str(path) + ".part") as archive:
            self.assertIsNone(archive.testzip())
        os.replace(path, self.root / "old.cbz")

    def run_update(self):
        return manga.download_album(self.client, "123", str(self.dest), str(self.root / "work"))

    def test_old_format_and_manifest_reuse_original_pages(self):
        with patch.object(manga, "option_for", side_effect=lambda p: p), patch.object(manga.jmcomic, "download_photo", side_effect=self.downloaded) as download, patch.object(manga, "trash", side_effect=self.move_old):
            self.assertTrue(self.run_update())
            self.assertEqual([c.args[0] for c in download.call_args_list], ["3"])
            self.assertEqual((self.root / "old.cbz").read_bytes(), self.original)
            # A renamed chapter is still recognized by its saved stable ID.
            self.photos[0].name = "renamed"
            self.photos.append(SimpleNamespace(photo_id="4", index=4, name="chapter4"))
            self.client.get_album_detail.return_value = Album(self.photos)
            download.reset_mock()
            self.assertTrue(self.run_update())
            self.assertEqual([c.args[0] for c in download.call_args_list], ["4"])
            with zipfile.ZipFile(self.book) as archive:
                self.assertEqual(archive.read("0001_renamed/001.jpg"), b"original-1")
                self.assertEqual(archive.read("0002_chapter2/001.jpg"), b"original-2")
                self.assertEqual(archive.read("cover.jpg"), b"original-cover")
                self.assertEqual(len(json.loads(archive.read("metadata.json"))["chapters"]), 4)
            download.reset_mock()
            self.assertTrue(self.run_update())
            download.assert_not_called()

    def test_download_failure_does_not_replace_original(self):
        with patch.object(manga, "option_for", side_effect=lambda p: p), patch.object(manga.jmcomic, "download_photo", side_effect=RuntimeError("offline")), patch.object(manga.time, "sleep"), patch.object(manga, "trash") as trash:
            self.assertFalse(self.run_update())
            self.assertEqual(self.book.read_bytes(), self.original)
            trash.assert_not_called()

    def test_online_chapter_removed_does_not_discard_local_pages(self):
        self.client.get_album_detail.return_value = Album(self.photos[1:])
        with patch.object(manga.jmcomic, "download_photo") as download, patch.object(manga, "trash") as trash:
            with self.assertRaisesRegex(RuntimeError, "保留原包"):
                self.run_update()
            self.assertEqual(self.book.read_bytes(), self.original)
            download.assert_not_called()
            trash.assert_not_called()


if __name__ == "__main__":
    unittest.main()

"""Music filtering tests: block remixes, live, covers, sped-up, and unofficial releases."""
import importlib.util
from pathlib import Path
import sys
import unittest
from unittest.mock import patch

SCRIPTS_DIR = Path(__file__).parents[1] / "scripts"
if str(SCRIPTS_DIR) not in sys.path:
    sys.path.insert(0, str(SCRIPTS_DIR))
spec = importlib.util.spec_from_file_location("ytmusic", SCRIPTS_DIR / "ytmusic.py")
ytmusic = importlib.util.module_from_spec(spec)
spec.loader.exec_module(ytmusic)

gen_spec = importlib.util.spec_from_file_location("general", SCRIPTS_DIR / "general.py")
general = importlib.util.module_from_spec(gen_spec)
gen_spec.loader.exec_module(general)


class VersionFilterTests(unittest.TestCase):
    def test_rejects_derivative_and_live_titles(self):
        bad = [
            "Blinding Lights (Remix)",
            "Blinding Lights - Tiësto Remix",
            "Song (Extended Mix)",
            "Song (Club Edit)",
            "Song (Sped Up)",
            "Song - Slowed + Reverb",
            "Song (Nightcore)",
            "Song (Live)",
            "Song - Live at Wembley",
            "Song (MTV Unplugged)",
            "Song (Acoustic)",
            "Song - Acoustic Version",
            "Song (Instrumental)",
            "Butterfly Instrumental",
            "Snowman Pt. 1 (Lofi Soundscape)",
            "Song (Karaoke Version)",
            "Song (Piano Cover)",
            "Song (Demo)",
            "Song (Commentary)",
            "Winter Shopping loop station ver.",
            "Shakira DJ Memegamix",
            "晴天 (现场版)",
            "稻香 (翻唱)",
            "夜曲 (伴奏)",
            "泡沫 (加速版)",
        ]
        for title in bad:
            with self.subTest(title=title):
                self.assertTrue(ytmusic.is_non_original(title))

    def test_keeps_legitimate_studio_titles_and_remasters(self):
        good = [
            "Live Forever",
            "Cover Me",
            "Alive",
            "Shallow (Radio Edit)",
            "Who Knew (Edit)",
            "Song (2011 Remaster)",
            "Song - Remastered 2023",
            "Song (feat. IU)",
            "Song (Album Version)",
            "Song (Explicit)",
        ]
        for title in good:
            with self.subTest(title=title):
                self.assertFalse(ytmusic.is_non_original(title))

    def test_short_artist_name_does_not_substring_match_unrelated_artists(self):
        self.assertFalse(ytmusic._same_artist(["IU"], ["Various Artists"]))
        self.assertFalse(ytmusic._same_artist(["V"], ["Cover Band"]))
        self.assertFalse(ytmusic._same_artist(["AI"], ["Kawaii Project"]))
        self.assertTrue(ytmusic._same_artist(["IU"], ["IU, SUGA"]))
        self.assertTrue(ytmusic._same_artist(["Taylor Swift"], ["Taylor Swift - Topic"]))


class ResolveAndAlbumTests(unittest.TestCase):
    def setUp(self):
        ytmusic._albums.clear()
        ytmusic._titles.clear()

    def test_audio_version_ignores_remix_live_cover_and_picks_studio_atv(self):
        search_results = [
            {
                "videoId": "remix1",
                "title": "Shape of You (DJ Remix)",
                "videoType": "MUSIC_VIDEO_TYPE_ATV",
                "resultType": "song",
                "artists": [{"name": "Ed Sheeran"}],
                "album": {"id": "MPREb_remix", "name": "Shape of You (Remixes)"},
            },
            {
                "videoId": "live1",
                "title": "Shape of You (Live)",
                "videoType": "MUSIC_VIDEO_TYPE_ATV",
                "resultType": "song",
                "artists": [{"name": "Ed Sheeran"}],
                "album": {"id": "MPREb_live", "name": "Live Tour"},
            },
            {
                "videoId": "omv1",
                "title": "Shape of You",
                "videoType": "MUSIC_VIDEO_TYPE_OMV",
                "resultType": "video",
                "artists": [{"name": "Ed Sheeran"}],
                "album": {"id": "MPREb_orig", "name": "Divide"},
            },
            {
                "videoId": "studio1",
                "title": "Shape of You",
                "videoType": "MUSIC_VIDEO_TYPE_ATV",
                "resultType": "song",
                "artists": [{"name": "Ed Sheeran"}],
                "album": {"id": "MPREb_orig", "name": "Divide"},
            },
        ]
        with patch.object(ytmusic, "call", return_value=search_results):
            found = ytmusic.audio_version("Shape of You", ["Ed Sheeran"])
        self.assertIsNotNone(found)
        self.assertEqual(found["videoId"], "studio1")

    def test_resolve_blocks_unofficial_and_derivative_atv(self):
        watch = {
            "tracks": [
                {
                    "videoId": "fake_atv",
                    "title": "Blinding Lights (Piano Cover)",
                    "videoType": "MUSIC_VIDEO_TYPE_ATV",
                    "artists": [{"name": "Relaxing Piano Crew"}],
                    "album": {"id": "MPREb_tribute", "name": "Piano Tribute"},
                }
            ]
        }
        with patch.object(ytmusic, "call", return_value=watch):
            target, _, reason = ytmusic.resolve("fake_atv")
        self.assertIsNone(target)
        self.assertIn("非原版", reason)

    def test_resolve_blocks_spam_compilation_album_even_if_track_title_clean(self):
        watch = {
            "tracks": [
                {
                    "videoId": "comp_atv",
                    "title": "Song A",
                    "videoType": "MUSIC_VIDEO_TYPE_ATV",
                    "artists": [{"name": "Artist A"}],
                    "album": {"id": "MPREb_spam", "name": "TikTok Viral Hits 2026"},
                }
            ]
        }
        spam_album = {
            "title": "TikTok Viral Hits 2026",
            "type": "Album",
            "year": "2026",
            "artists": [{"name": "Various Artists"}],
            "tracks": [{"videoId": "comp_atv", "title": "Song A", "trackNumber": 1}],
        }

        def fake_call(method, *args, **kwargs):
            if method == "get_watch_playlist":
                return watch
            if method == "get_album":
                return spam_album
            raise AssertionError(method)

        with patch.object(ytmusic, "call", side_effect=fake_call):
            target, _, reason = ytmusic.resolve("comp_atv")
        self.assertIsNone(target)
        self.assertIn("非原版", reason)

    def test_resolve_uses_counterpart_and_blocks_live_or_remix_source_video(self):
        mv_watch = {
            "tracks": [
                {
                    "videoId": "mv_id",
                    "title": "Song A",
                    "videoType": "MUSIC_VIDEO_TYPE_OMV",
                    "artists": [{"name": "Artist A"}],
                    "counterpart": {
                        "videoId": "atv_id",
                        "title": "Song A",
                        "videoType": "MUSIC_VIDEO_TYPE_ATV",
                        "artists": [{"name": "Artist A"}],
                        "album": {"id": "MPREb_a", "name": "Album A"},
                    },
                }
            ]
        }
        studio_album = {
            "title": "Album A",
            "type": "Album",
            "year": "2024",
            "artists": [{"name": "Artist A"}],
            "tracks": [{"videoId": "atv_id", "title": "Song A", "trackNumber": 1}],
        }

        def fake_call(method, *args, **kwargs):
            if method == "get_watch_playlist":
                return mv_watch
            if method == "get_album":
                return studio_album
            raise AssertionError(method)

        with patch.object(ytmusic, "call", side_effect=fake_call), patch.object(ytmusic, "youtube_title", return_value="Artist A - Song A (Official Video)"):
            target, info, note = ytmusic.resolve("mv_id")
        self.assertEqual(target, "atv_id")
        self.assertEqual(info["album"], "Album A")
        self.assertIn("纯音轨版", note)

        # If YouTube title is Live, do not swap to studio ATV
        with patch.object(ytmusic, "call", side_effect=fake_call), patch.object(ytmusic, "youtube_title", return_value="Artist A - Song A (Live at Tokyo Dome)"):
            target2, _, _ = ytmusic.resolve("mv_id")
        self.assertIsNone(target2)

    def test_resolve_official_audio_fallback_rejects_sped_up_or_remix(self):
        omv_watch = {
            "tracks": [
                {
                    "videoId": "omv_sped",
                    "title": "Song A",
                    "videoType": "MUSIC_VIDEO_TYPE_OMV",
                    "artists": [{"name": "Artist A"}],
                }
            ]
        }

        def fake_call(method, *args, **kwargs):
            if method == "get_watch_playlist":
                return omv_watch
            if method == "search":
                return []
            raise AssertionError(method)

        with patch.object(ytmusic, "call", side_effect=fake_call), patch.object(ytmusic, "youtube_title", return_value="Artist A - Song A (Sped Up) [Official Audio]"):
            target, _, _ = ytmusic.resolve("omv_sped")
        self.assertIsNone(target)

        with patch.object(ytmusic, "call", side_effect=fake_call), patch.object(ytmusic, "youtube_title", return_value="Artist A - Song A [Official Audio]"):
            target_ok, _, _ = ytmusic.resolve("omv_sped")
        self.assertEqual(target_ok, "omv_sped")

    def test_artist_skips_remix_live_releases_and_dedups_remasters(self):
        artist_page = {
            "name": "Artist A",
            "albums": {
                "results": [
                    {"browseId": "MPREb_2018", "title": "Debut", "year": "2018"},
                    {"browseId": "MPREb_2020_live", "title": "Debut (Live at Budokan)", "year": "2020"},
                    {"browseId": "MPREb_2020_sleep", "title": "Debut (Sleep Soundscape)", "year": "2020"},
                    {"browseId": "MPREb_2022_remaster", "title": "Debut (Deluxe)", "year": "2022"},
                ]
            },
            "singles": {
                "results": [
                    {"browseId": "MPREb_remix_ep", "title": "Song One (Remixes)", "year": "2021"},
                ]
            },
            "videos": {},
        }
        albums = {
            "MPREb_2018": {
                "title": "Debut",
                "type": "Album",
                "year": "2018",
                "artists": [{"id": "UC_A", "name": "Artist A"}],
                "tracks": [
                    {"videoId": "t1", "title": "Song One", "trackNumber": 1, "videoType": "MUSIC_VIDEO_TYPE_ATV"},
                    {"videoId": "t1_inst", "title": "Song One (Instrumental)", "trackNumber": 2, "videoType": "MUSIC_VIDEO_TYPE_ATV"},
                ],
            },
            "MPREb_remix_ep": {
                "title": "Song One (Remixes)",
                "type": "EP",
                "year": "2021",
                "artists": [{"id": "UC_A", "name": "Artist A"}],
                "tracks": [
                    {"videoId": "t1_dup", "title": "Song One", "trackNumber": 1, "videoType": "MUSIC_VIDEO_TYPE_ATV"},
                    {"videoId": "t1_club", "title": "Song One (Club Remix)", "trackNumber": 2, "videoType": "MUSIC_VIDEO_TYPE_ATV"},
                ],
            },
            "MPREb_2022_remaster": {
                "title": "Debut (Deluxe)",
                "type": "Album",
                "year": "2022",
                "artists": [{"id": "UC_A", "name": "Artist A"}],
                "tracks": [
                    {"videoId": "t1_remaster", "title": "Song One - 2022 Remaster", "trackNumber": 1, "videoType": "MUSIC_VIDEO_TYPE_ATV"},
                    {"videoId": "t2_demo", "title": "Song Two - Demo", "trackNumber": 2, "videoType": "MUSIC_VIDEO_TYPE_ATV"},
                    {"videoId": "t2", "title": "Song Two", "trackNumber": 3, "videoType": "MUSIC_VIDEO_TYPE_ATV"},
                ],
            },
        }

        def fake_call(method, *args, **kwargs):
            if method == "get_artist":
                return artist_page
            if method == "get_album":
                return albums[args[0]]
            raise AssertionError((method, args))

        logs = []
        with patch.object(ytmusic, "call", side_effect=fake_call):
            res = ytmusic.artist("UC_A", logs.append)
        vids = [vid for vid, _ in res["songs"]]
        self.assertEqual(vids, ["t1", "t2"])


class MetricFilterTests(unittest.TestCase):
    def _ns(self, min_views=1000000, min_replay=5.0, no_exempt_albums=False):
        from types import SimpleNamespace
        return SimpleNamespace(
            min_views=min_views,
            min_replay=min_replay,
            no_exempt_albums=no_exempt_albums,
            music_skipped=[],
            music_filtered=[],
            lastfm_known={},
        )

    def test_requires_both_min_views_and_min_replay_when_enabled(self):
        a = self._ns(min_views=1000000, min_replay=5.0)
        candidates = [
            ("v_album", {"title": "Deep Cut Intro", "artists": ["Artist X"], "album_exempt": True}),
            ("v_both_ok", {"title": "Shallow (Radio Edit)", "artists": ["Lady Gaga, Bradley Cooper"]}),
            ("v_low_views", {"title": "Niche Gem", "artists": ["Artist A"]}),
            ("v_low_replay", {"title": "Popular Skip", "artists": ["Artist B"]}),
            ("v_low_listeners", {"title": "Tiny Listeners", "artists": ["Artist C"]}),
            ("v_no_lf", {"title": "Missing Lastfm", "artists": ["Artist D"]}),
        ]
        views_map = {
            "v_both_ok": 50_000_000,
            "v_low_views": 500_000,
            "v_low_replay": 5_000_000,
            "v_low_listeners": 2_000_000,
            "v_no_lf": 3_000_000,
        }
        lf_map = {
            ("Lady Gaga", "Shallow"): {"listeners": 500000, "playcount": 5000000},
            ("Artist B", "Popular Skip"): {"listeners": 1000, "playcount": 4000},
            ("Artist C", "Tiny Listeners"): {"listeners": 30, "playcount": 300},
            ("Artist D", "Missing Lastfm"): None,
        }

        def fake_lastfm_many(pairs, key):
            return [lf_map.get(p) for p in pairs]

        with patch.object(general, "video_views", side_effect=lambda vid, opts: views_map[vid]), \
                patch.dict("os.environ", {"OMNIHUB_LASTFM_KEY": "testkey"}), \
                patch("rank.lastfm_many", side_effect=fake_lastfm_many):
            kept = general.filter_music_metrics(candidates, a, {})

        self.assertEqual([vid for vid, _ in kept], ["v_album", "v_both_ok"])
        self.assertEqual(kept[0][1]["source_note"], "整张专辑保留")
        self.assertEqual(kept[1][1]["views"], 50_000_000)
        self.assertEqual(kept[1][1]["lastfm_replay"], 10.0)
        self.assertIn("v_both_ok", a.lastfm_known)
        self.assertEqual(len(a.music_filtered), 4)
        self.assertEqual(a.music_skipped, [])

    def test_album_exemption_can_be_disabled(self):
        a = self._ns(min_views=1000000, min_replay=0.0, no_exempt_albums=True)
        candidates = [("v_album", {"title": "Deep Cut Intro", "artists": ["Artist X"], "album_exempt": True})]
        with patch.object(general, "video_views", return_value=12000):
            kept = general.filter_music_metrics(candidates, a, {})
        self.assertEqual(kept, [])
        self.assertEqual(len(a.music_filtered), 1)
        self.assertIn("低于", a.music_filtered[0]["reason"])

    def test_thresholds_can_be_disabled(self):
        a = self._ns(min_views=0, min_replay=0.0)
        candidates = [("v1", {"title": "Any Song", "artists": ["Artist"]})]
        kept = general.filter_music_metrics(candidates, a, {})
        self.assertEqual([vid for vid, _ in kept], ["v1"])
        self.assertEqual(a.music_filtered, [])


if __name__ == "__main__":
    unittest.main()

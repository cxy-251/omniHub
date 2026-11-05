"""Player/start-point updates must preserve unrelated map bytes."""
from pathlib import Path
import random
import struct
import sys
import unittest

sys.path.insert(0, str(Path(__file__).parents[1]))
from starts import arrange_starts


def fixture():
    records = []
    for pid, control in ((0, 3), (1, 1), (2, 1), (3, 1), (4, 1), (15, 4)):
        records.append(struct.pack("<BII", pid, control, 0xFFFFFFFF) + b"\0" + struct.pack("<III", 0, 0, 0) + b"\0")
    data = b"IpaM" + struct.pack("<I", 0x27) + b"preserve-header\0" + struct.pack("<I", len(records)) + b"".join(records)
    data += struct.pack("<I4II", 4, 101, 102, 103, 104, 4) + b"preserve-tail"
    objects = b'<PlacedObjects><ObjectPoint Id="101" Type="StartLoc" Position="0,0,0"/><ObjectPoint Id="102" Type="StartLoc" Position="10,0,0"/><ObjectPoint Id="103" Type="StartLoc" Position="100,0,0"/><ObjectPoint Id="104" Type="StartLoc" Position="110,0,0"/></PlacedObjects>'
    return data, objects


class StartPositions(unittest.TestCase):
    def test_side_assignment_and_only_start_fields_change(self):
        original, objects = fixture()
        for teams in ([1, 1, 2, 2], [1, 2, 1, 2]):
            for seed in (1, 2, 3):
                patched, positions = arrange_starts(original, objects, teams, random.Random(seed))
                self.assertEqual(len(patched), len(original))
                self.assertEqual(len({p["point"] for p in positions}), 4)
                self.assertEqual([p["team"] for p in positions], teams)
                for first in positions:
                    for second in positions:
                        distance = abs(first["position"][0] - second["position"][0])
                        self.assertLessEqual(distance, 10) if first["team"] == second["team"] else self.assertGreaterEqual(distance, 90)
                restored = bytearray(patched)
                for player in range(1, 5):
                    marker = struct.pack("<BII", player, 1, 0xFFFFFFFF)
                    offset = original.index(marker) + 14
                    self.assertEqual(struct.unpack_from("<I", patched, offset)[0], positions[player - 1]["point"])
                    restored[offset:offset + 4] = original[offset:offset + 4]
                self.assertEqual(bytes(restored), original)

    def test_invalid_or_ambiguous_map_is_rejected(self):
        data, objects = fixture()
        for bad in (b"not a map", data + data, data[:4] + struct.pack("<I", 0x28) + data[8:]):
            with self.assertRaises(ValueError):
                arrange_starts(bad, objects, [1, 1, 2, 2])
        with self.assertRaises(ValueError):
            arrange_starts(data, objects, [1, 1, 1, 1])


if __name__ == "__main__":
    unittest.main()

"""One locally owned SC2 window, controlled by short-lived per-round clients."""
import argparse
import asyncio
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import threading
import time
from types import SimpleNamespace

from loguru import logger

from sc2tool import (_prepare_match, _pfx_docs, _replay_files, _has_finished_replay,
                     _force_kill_sc2, _sc2_pids, _watch_window)


class InterfaceExited(Exception):
    """The native menu left the engine without reporting an API result."""


async def forward_match(socket, payload):
    """Keep a round alive until its result; disconnecting requests surrender."""
    deadline = time.monotonic() + 180
    while True:
        try:
            reader, writer = await asyncio.open_unix_connection(socket)
            break
        except (FileNotFoundError, ConnectionRefusedError):
            if time.monotonic() >= deadline:
                raise RuntimeError("SC2 窗口服务未就绪")
            await asyncio.sleep(.2)
    try:
        writer.write((json.dumps({"op": "play", **payload}) + "\n").encode())
        await writer.drain()
        data = await reader.readline()
        if not data:
            raise RuntimeError("SC2 窗口服务已退出")
        return {**payload, **json.loads(data)}
    finally:
        writer.close()
        await writer.wait_closed()


class Host:
    """Own the native process; only one round can use its API connection."""

    def __init__(self):
        self.process = self.server = self.pending = self.round = None
        self.connections = set()
        self.busy = False
        self.playing = False
        self.closed = asyncio.Event()
        self.surrender = asyncio.Event()
        self.watch_stop = threading.Event()
        self.lost = {}
        self.replay_id = ""
        self.replay_error = ""
        self.replay_stop = asyncio.Event()
        self.replay_loop = 0

    async def rpc(self, call, timeout=30, game=None, before=None):
        pending = self.pending = asyncio.create_task(call)
        started = time.monotonic()
        try:
            while not (await asyncio.wait({pending}, timeout=2))[0]:
                if game is not None and _has_finished_replay(_pfx_docs() / "Replays", before, game.relative_path):
                    raise InterfaceExited()
                if time.monotonic() - started >= timeout:
                    raise RuntimeError("SC2 状态接口未响应，需要重新打开游戏窗口")
            return pending.result()
        finally:
            if pending.done() and self.pending is pending:
                self.pending = None

    async def open_game(self):
        if self.server is not None:
            return
        from sc2.sc2process import SC2Process
        self.process = SC2Process(fullscreen=True)
        self.server = await self.process.__aenter__()
        await self.rpc(self.server.ping())
        self.watch_stop = threading.Event()
        self.lost = {}
        threading.Thread(target=_watch_window, args=(self.watch_stop, self.lost, self.process._port), daemon=True).start()
        asyncio.create_task(self.monitor(self.process))

    async def monitor(self, process):
        while self.process is process and not self.closed.is_set():
            if not _sc2_pids(process._port):
                self.closed.set()
                return
            await asyncio.sleep(1)

    def window(self, idle):
        if self.process is None:
            return
        try:
            for pid in _sc2_pids(self.process._port):
                wins = subprocess.run(["xdotool", "search", "--pid", pid], capture_output=True, text=True, timeout=2).stdout.split()
                for win in wins:
                    geom = subprocess.run(["xdotool", "getwindowgeometry", "--shell", win], capture_output=True, text=True, timeout=2).stdout
                    dims = dict(line.split("=", 1) for line in geom.splitlines() if "=" in line)
                    if int(dims.get("WIDTH", 0)) < 400 or int(dims.get("HEIGHT", 0)) < 300:
                        continue
                    subprocess.run(["xdotool", "set_window", "--name", "StarCraft II · " + ("等待下一局 · omniHub" if idle else "omniHub"), win], capture_output=True, timeout=2)
                    subprocess.run(["xdotool", "windowminimize" if idle else "windowactivate", win], capture_output=True, timeout=2)
        except (OSError, subprocess.SubprocessError):
            logger.warning("未能自动切换 SC2 窗口，可在任务栏切换")

    async def stop_game(self):
        process, server = self.process, self.server
        self.process = self.server = None
        self.watch_stop.set()
        if process is None:
            return
        _force_kill_sc2(process._port)
        if server is not None:
            await server._ws.close()
        if self.pending is not None:
            await asyncio.gather(self.pending, return_exceptions=True)
            self.pending = None
        await process.__aexit__(None, None, None)

    async def play(self, payload):
        from sc2.client import Client
        from s2clientprotocol import debug_pb2 as debug, sc2api_pb2 as api
        result = {k: payload[k] for k in ("map", "race", "opponents", "mods")}
        try:
            args = SimpleNamespace(map=payload["map"], race=payload["race"],
                                   opponents=json.dumps(payload["opponents"]), mods=",".join(payload["mods"]), reusable=True)
            game, players = await asyncio.to_thread(_prepare_match, args)
            reused = self.server is not None
            await self.open_game()
            before = _replay_files(_pfx_docs() / "Replays")
            created = await self.rpc(self.server.create_game(game, players, realtime=True), timeout=180)
            if created.create_game.HasField("error"):
                raise RuntimeError(str(created.create_game))
            client = Client(self.server._ws)
            player = await self.rpc(client.join_game(race=players[0].race), timeout=180)
            self.playing = True
            self.window(False)
            logger.info("对局开始，{}游戏窗口", "复用" if reused else "新建")
            while True:
                if self.surrender.is_set():
                    await self.rpc(client._execute(debug=api.RequestDebug(debug=[debug.DebugCommand(
                        end_game=debug.DebugEndGame(end_result=debug.DebugEndGame.Surrender))])))
                    self.surrender.clear()
                await self.rpc(client.observation(), game=game, before=before)
                if client._game_result:
                    result["result"] = client._game_result[player].name
                    break
                await asyncio.sleep(.2)
            try:
                import replays
                saved = await self.rpc(client._execute(save_replay=api.RequestSaveReplay()))
                result["replay_id"] = await asyncio.to_thread(replays.save, saved.save_replay.data, game.path, result)
            except Exception as error:
                result["replay_error"] = "回放保存失败：" + str(error)
                logger.warning(result["replay_error"])
            await self.rpc(client.leave())
            self.window(True)
            result["window_reused"] = reused
            logger.info("本局结束：{}；窗口保留，等待下一局", result["result"])
        except asyncio.CancelledError:
            result["result"] = "Quit"
        except InterfaceExited:
            result["result"] = "Quit"
            logger.warning("游戏菜单直接退出了引擎，将在下一局重新打开窗口")
            await self.stop_game()
            self.closed.set()
        except Exception as error:
            result.update(result="Error", error=self.lost.get("reason") or str(error))
            logger.error("对局失败：{}", result["error"])
            await self.stop_game()
            self.closed.set()
        finally:
            self.playing = False
            self.busy = False
        return result

    async def watch_replay(self, replay_id):
        from s2clientprotocol import sc2api_pb2 as api
        import replays
        try:
            folder = replays.bundle(replay_id)
            await self.open_game()
            data = (folder / "game.SC2Replay").read_bytes()
            info = await self.rpc(self.server._execute(replay_info=api.RequestReplayInfo(replay_data=data)))
            if info.replay_info.HasField("error"):
                raise RuntimeError(str(info.replay_info))
            duration = info.replay_info.game_duration_loops
            response = await self.rpc(self.server._execute(start_replay=api.RequestStartReplay(
                replay_data=data,
                map_data=(folder / "map.SC2Map").read_bytes(), observed_player_id=1,
                realtime=True, options=api.InterfaceOptions(raw=True, score=True))), timeout=180)
            if response.start_replay.HasField("error"):
                raise RuntimeError(str(response.start_replay))
            self.window(False)
            while not self.replay_stop.is_set():
                response = await self.rpc(self.server._execute(observation=api.RequestObservation()))
                self.replay_loop = response.observation.observation.game_loop
                # Realtime replay UI can stay in in_replay at the final frame.
                if response.status == api.ended or (duration > 0 and response.observation.observation.game_loop >= duration):
                    break
                # SC2 replay mode needs step requests even when realtime=True
                # (the regular human-match loop does not).
                await self.rpc(self.server._execute(step=api.RequestStep(count=8)))
                await asyncio.sleep(.01)
            await self.rpc(self.server._execute(leave_game=api.RequestLeaveGame()))
            self.window(True)
        except asyncio.CancelledError:
            raise
        except Exception as error:
            self.replay_error = str(error)
            logger.error("回放失败：{}", error)
            await self.stop_game()
        finally:
            self.replay_id = ""
            self.busy = False

    async def handle(self, reader, writer):
        connection = asyncio.current_task()
        self.connections.add(connection)
        eof = None
        try:
            data = await asyncio.wait_for(reader.readline(), 5)
            command = json.loads(data)
            op = command.get("op")
            if op == "status":
                result = {"busy": self.busy, "playing": self.playing,
                          "port": self.process._port if self.process is not None else None,
                          "replay_id": self.replay_id, "replay_error": self.replay_error,
                          "replay_loop": self.replay_loop}
            elif op == "close":
                self.closed.set()
                result = {"ok": True}
            elif op == "surrender":
                if self.busy:
                    self.surrender.set()
                result = {"ok": True}
            elif op == "stop_replay":
                self.replay_stop.set()
                result = {"ok": True}
            elif op == "replay":
                import replays
                if self.busy or self.closed.is_set():
                    raise RuntimeError("请先结束当前对局或回放")
                replay_id = command.get("id")
                replays.bundle(replay_id)
                self.replay_id, self.replay_error = replay_id, ""
                self.replay_stop = asyncio.Event()
                self.replay_loop = 0
                self.busy = True
                self.round = asyncio.create_task(self.watch_replay(replay_id))
                result = {"ok": True}
            elif op == "play":
                if self.busy or self.closed.is_set():
                    raise RuntimeError("SC2 已有对局在进行或正在关闭")
                if not isinstance(command.get("opponents"), list) or not 1 <= len(command["opponents"]) <= 7:
                    raise ValueError("无效电脑配置")
                if not isinstance(command.get("map"), str) or command.get("race") not in ("T", "P", "Z", "R") or not isinstance(command.get("mods"), list):
                    raise ValueError("无效对局配置")
                self.busy = True
                self.surrender = asyncio.Event()
                self.round = asyncio.create_task(self.play(command))
                eof = asyncio.create_task(reader.read(1))
                done, _ = await asyncio.wait({self.round, eof}, return_when=asyncio.FIRST_COMPLETED)
                if eof in done and not self.round.done():
                    self.surrender.set()
                result = await self.round
            else:
                raise ValueError("无效 SC2 操作")
            writer.write((json.dumps(result, ensure_ascii=False) + "\n").encode())
            await writer.drain()
        except (ConnectionError, BrokenPipeError):
            pass
        except Exception as error:
            writer.write((json.dumps({"result": "Error", "error": str(error)}) + "\n").encode())
            await writer.drain()
        finally:
            if eof is not None:
                eof.cancel()
                await asyncio.gather(eof, return_exceptions=True)
            writer.close()
            try:
                await writer.wait_closed()
            except ConnectionError:
                pass
            self.connections.discard(connection)


async def serve(socket):
    host = Host()
    loop = asyncio.get_running_loop()
    for sig in (signal.SIGTERM, signal.SIGINT):
        loop.add_signal_handler(sig, host.closed.set)
    server = await asyncio.start_unix_server(host.handle, path=socket, limit=16384)
    os.chmod(socket, 0o600)
    try:
        await host.closed.wait()
    finally:
        server.close()
        await server.wait_closed()
        if host.round is not None and not host.round.done():
            host.round.cancel()  # rpc waits without cancelling the underlying SC2 request.
        await host.stop_game()
        if host.round is not None:
            await asyncio.gather(host.round, return_exceptions=True)
        if host.connections:
            _, pending = await asyncio.wait(host.connections, timeout=2)
            for task in pending:
                task.cancel()
            await asyncio.gather(*pending, return_exceptions=True)
        Path(socket).unlink(missing_ok=True)


if __name__ == "__main__":
    os.environ.setdefault("SC2PF", "WineLinux")
    os.environ.setdefault("SC2_TIMEOUT", "180")
    logger.remove()
    logger.add(sys.stderr, level="INFO")
    parser = argparse.ArgumentParser()
    parser.add_argument("--socket", required=True)
    asyncio.run(serve(parser.parse_args().socket))

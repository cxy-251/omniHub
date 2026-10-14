# 漫画封箱与索引

服务默认注册两个任务，无需编辑 tasks.json：

- **漫画图片目录封箱（CBZ）**：选择可写资源库和 manga 分类下的一本书目录，输入新的 `.cbz` 文件名。需要已有 uv 与 Python >=3.10；离线运行，不自动安装工具、解释器或依赖。
- **漫画 CBZ 索引更新**：合并所选目录及子目录的现存 CBZ 元数据，供旧书导入和索引修复使用。此任务由 Go 执行，不需要 Python。

准备目录示例：

```text
media_library/manga/作品/
  cover.jpg
  metadata.json       可选；缺省使用目录名作为标题
  ComicInfo.xml       可选；原样保留到 CBZ
  第1话/
    001.jpg
    002.jpg
  第2话/
    001.webp
```

根目录必须已有官方封面 `cover.jpg` 或 `cover.webp`，至少有一张阅读页。封箱器不下载封面、不把任意页面冒充官方封面；网络爬虫仍待后续交付。支持 JPG/JPEG/PNG/WebP/GIF，按数字自然序组织，不重新编码或改变原图。已有 metadata.json 保留，缺省生成标题；ComicInfo.xml 同时参与标题、作者、简介及 Genre/Tags 标签解析；JSON 明确提供的字段（包括空值/false）优先。章节数缺省按实际页目录统计，不把 ComicInfo 的系列 Count 当成当前书的章节数或完结证明。

在 Web 任务台选择“作品”目录、输出 `作品.cbz` 后启动。完整写入临时 ZIP、同步并校验后才原子发布；已有目标（包括链接）拒绝覆盖。成功发布自动更新分类根目录的 manga.db，刷新漫画书架即可读取。封箱和索引任务对整个 manga 分类互斥，避免不同书目录中的任务竞争同一索引。

源图与源元数据从不删除或覆盖。图片按 64KiB 流式复制，采用 ZIP Stored 保留原编码，减少重复压缩占用；日志最多约每秒一条进度。最多 10,000 图片/20,000 遍历项、32 层目录、单图 32MiB、总源文件 2GiB；路径元数据有独立预算。拒绝符号链接、大小变化、文件头与扩展名不符、非法输出名，以及无效/超像素预算的封面。普通取消会清理本次临时文件；强制终止/断电可能遗留 `.omnihub-pack-*.tmp`，不会发布未完成的 CBZ。取消与最终发布同时发生时，完整包可能已发布，不会为撤销取消而删除有效文件。

文件发布与 SQLite 不是同一事务。发布后若目录同步或索引失败，任务明确报错并保留完整包；修复存储问题后运行索引更新。索引按相对路径合并，保留旧 ID、手动封面和历史；新记录用路径摘要区分同名书和重复包内 ID。重复扫描相同元数据跳过行更新。索引不会删除旧记录，遇到损坏包/重复旧路径会报告错误及此前完成数量，不悄悄丢弃历史。

Python 源码位于本模块 scripts/pack.py，由 Go 嵌入并交给 uv 标准输入执行；通过 Python -I -S 隔离导入路径，不从素材目录或用户 site 导入模块；运行时不在媒体旁部署脚本/虚拟环境。uv 的任务缓存位于服务配置目录 task-cache/uv，仅任务运行时使用。Windows/macOS 服务可编译，实际工具安装、取消、磁盘原子发布尚待对应系统验收；不对未验证的文件系统提供兼容性承诺。

必要验证（使用项目隔离目录）：

```sh
TMPDIR="$PWD/.test-tmp" go build -o .build/omnihub-pack-check ./cmd/server
OMNIHUB_PACK_CHECK="$PWD/.build/omnihub-pack-check" TMPDIR="$PWD/.test-tmp" \
  go test ./features/manga ./internal/manga ./internal/scanner ./internal/tasks ./internal/server ./internal/application
```

实际进程检查覆盖自然序、中文/空格目录、字节保留、发布后索引、索引重建、拒绝覆盖/坏封面、普通取消清理；不依赖用户媒体。预览包检查可额外设置 OMNIHUB_PACK_PREVIEW_FIXTURE 为项目内 JPEG 样本，验证默认注册任务、书架 API、页序和封面返回。

## 无索引与只读浏览

没有 manga.db 的库可以直接按页浏览 CBZ，包括只读库；读取标题/作者、封面、页序及全局账号进度，不创建库内数据库或侧车。目录沿用按需受限缓存，单页最多 100 项，整个请求逐包读取；同时最多两个请求解析发现元数据。元数据缓存保守估算上限 4MiB/128 本，活动访问检查文件身份、权限模式、大小和修改时间，闲置不轮询，不保留文件句柄。已有索引仍优先使用；向已有索引库添加外部文件后可显式运行索引更新。

坏包或元数据错误按条目标记，不阻塞同页其他书；可尝试打开有效页面或修复源文件。ComicInfo 限制 1MiB、64 层及 50,000 token，禁止 DTD/实体声明，不访问外部资源。支持 UTF-8，未知/不支持的编码明确报错。字段依据 [ComicInfo 2.0 schema](https://raw.githubusercontent.com/anansi-project/comicinfo/main/schema/v2.0/ComicInfo.xsd)，另兼容常见 Tags 扩展；完结状态由 JSON 提供，未从系列总册数猜测。

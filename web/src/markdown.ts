let marked: Promise<typeof import('marked')> | undefined;

// Markdown 渲染器（marked，版本由 pnpm-lock 固定）按需加载一次；输出经 DOMPurify 清理。
export async function renderMarkdown(text: string): Promise<string> {
  marked ??= import('marked');
  const [{ default: DOMPurify }, { marked: renderer }] = await Promise.all([import('dompurify'), marked]);
  return DOMPurify.sanitize(await renderer.parse(text));
}

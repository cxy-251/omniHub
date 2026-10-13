type Marked = { parse(text: string): string | Promise<string> };
let ready: Promise<void> | undefined;

// Load the bundled renderer once; lazy pages share its code and sanitized output.
export async function renderMarkdown(text: string): Promise<string> {
  if (!ready) ready = new Promise((resolve, reject) => {
    const script = document.createElement('script');
    script.src = '/vendor/marked.min.js';
    script.onload = () => resolve();
    script.onerror = () => { script.remove(); ready = undefined; reject(new Error('Markdown 渲染器加载失败')); };
    document.head.append(script);
  });
  const [{ default: DOMPurify }] = await Promise.all([import('dompurify'), ready]);
  const marked = (window as Window & { marked?: Marked }).marked;
  if (!marked) throw new Error('Markdown 渲染器不可用');
  return DOMPurify.sanitize(await marked.parse(text));
}

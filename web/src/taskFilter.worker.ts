// User regex runs outside the UI thread. The owner terminates this worker if a
// pathological expression exceeds its time budget; no log ever reaches disk.
self.onmessage = (event: MessageEvent<{ entries: { id: number; message: string }[]; pattern: string }>) => {
  try {
    const regex = new RegExp(event.data.pattern, 'i');
    self.postMessage({ ids: event.data.entries.filter(entry => regex.test(entry.message)).map(entry => entry.id) });
  } catch (error) { self.postMessage({ error: (error as Error).message }); }
};
export {};

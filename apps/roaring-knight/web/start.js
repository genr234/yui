const runtime = new URL("./_runtime/roaring-knight/", location.href);
const options = new URLSearchParams(location.search);
const fight = options.get("fight") || "knight";
const chapters = new Map([
  ["king", 1], ["jevil", 1],
  ["queen", 2], ["giga-queen", 2], ["spamton", 2], ["spamton-neo", 2],
  ["tenna", 3], ["knight", 3], ["eram", 3],
  ["jackenstein", 4], ["sound-of-justice", 4], ["titan", 4], ["gerson", 4],
]);

function seedCheckpoint(contents) {
  return new Promise((resolve, reject) => {
    // The GameMaker web runner mounts this store as /_savedata.
    const request = indexedDB.open("/_savedata", 21);
    request.onupgradeneeded = () => {
      request.result.createObjectStore("FILE_DATA").createIndex("timestamp", "timestamp");
    };
    request.onerror = () => reject(request.error);
    request.onsuccess = () => {
      const db = request.result;
      const transaction = db.transaction("FILE_DATA", "readwrite");
      transaction.oncomplete = () => { db.close(); resolve(); };
      transaction.onabort = () => { db.close(); reject(transaction.error); };
      transaction.objectStore("FILE_DATA").put({
        timestamp: new Date(),
        mode: 0o100666,
        contents,
      }, "/_savedata/filech3_0");
    };
  });
}

async function start() {
  const chapter = chapters.get(fight);
  if (!chapter) throw new Error("That fight is unavailable.");
  const entry = new URL(chapter === 3 ? "index.html" : `chapter${chapter}/index.html`, runtime);
  const run = options.get("run");
  entry.search = run ? `yui-${fight}&yui-run=${encodeURIComponent(run)}` : `yui-${fight}`;
  const check = await fetch(entry, { method: "HEAD" });
  if (!check.ok) throw new Error("The installed runtime is missing this fight. Install the latest runtime ZIP.");

  if (chapter === 3) {
    const response = await fetch(new URL("filech3_0", runtime));
    if (!response.ok) throw new Error("The installed runtime is missing its Chapter 3 checkpoint.");
    const contents = new Uint8Array(await response.arrayBuffer());
    if (!contents.length) throw new Error("The installed Chapter 3 checkpoint is empty.");
    await seedCheckpoint(contents);
  }
  location.replace(entry);
}

start().catch((error) => {
  document.querySelector('[role="status"]').textContent = error.message;
});

const canvas = document.querySelector("canvas");
const brush = canvas.getContext("2d");
const run = new URLSearchParams(location.search).get("run");
let directions = {};
let lastInput = 0;
let lastFrame = performance.now();
let x = canvas.width / 2;
let y = canvas.height / 2;

window.yui.on((message) => {
  if (message?.type !== "head-input" || message.run !== run) return;
  directions = message.directions || {};
  lastInput = performance.now();
});
window.yui.ready.then(() => window.yui.post({ type: "head-input-ready", run }));
window.addEventListener("keydown", (event) => {
  if (event.code === "KeyR" && !event.repeat) window.yui.post({ type: "head-recenter", run });
});

function draw(now) {
  const step = Math.min(0.05, (now - lastFrame) / 1000) * 135;
  lastFrame = now;
  if (now - lastInput > 250) directions = {};
  x = Math.min(canvas.width - 9, Math.max(9, x + step * (Number(!!directions.right) - Number(!!directions.left))));
  y = Math.min(canvas.height - 9, Math.max(9, y + step * (Number(!!directions.down) - Number(!!directions.up))));
  brush.clearRect(0, 0, canvas.width, canvas.height);
  brush.fillStyle = "#ff334f";
  brush.beginPath();
  brush.moveTo(x, y + 8);
  brush.bezierCurveTo(x - 21, y - 5, x - 4, y - 15, x, y - 6);
  brush.bezierCurveTo(x + 4, y - 15, x + 21, y - 5, x, y + 8);
  brush.fill();
  requestAnimationFrame(draw);
}
requestAnimationFrame(draw);

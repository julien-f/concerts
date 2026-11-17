// One run of measure-scroll.mjs, which picks the CSS and computes the statistics: scrolls at a fixed
// speed for DURATION, posts the requestAnimationFrame intervals, then loads the next run.
const SPEED = 1500; // CSS px/s, from time, so a slow variant covers the same content
const DURATION = 4000;
const { run, runs, nonce } = document.currentScript.dataset;

// sendBeacon survives the navigation right after it; Firefox cancels a fetch there.
const post = (o) => navigator.sendBeacon("/results", JSON.stringify({ nonce, run: Number(run), ...o }));

addEventListener("DOMContentLoaded", () => {
  const note = document.createElement("p");
  document.querySelector("main").prepend(note);
  if (run === "") {
    const start = document.createElement("button");
    start.textContent = "Start";
    start.onclick = () => location.assign("/?run=0");
    note.append(start);
    return;
  }
  note.textContent = `Run ${Number(run) + 1}/${runs}, don't touch the screen`;

  // A touch or a hidden page spoils the run: it starts over.
  let spoiled = false;
  addEventListener("pointerdown", () => (spoiled = true));
  document.addEventListener("visibilitychange", () => (spoiled = true));

  scrollTo(0, 300); // past the point where the browser's toolbar collapses
  setTimeout(() => {
    const dt = [];
    let t0, last, y0;
    const tick = (t) => {
      if (spoiled) return location.replace(`/?run=${run}`);
      if (t0 === undefined) [t0, y0] = [t, scrollY];
      else dt.push(t - last);
      last = t;
      if (t - t0 >= DURATION) {
        post({ ua: navigator.userAgent, dt });
        return location.assign(`/?run=${Number(run) + 1}`);
      }
      const y = y0 + (SPEED * (t - t0)) / 1000;
      scrollTo(0, y);
      if (scrollY < y - 1) {
        post({ error: "reached the bottom of the list: make it longer (measure-scroll.mjs, snapshot)" });
        note.textContent = "List too short, see the terminal";
        return;
      }
      requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  }, 1500);
});

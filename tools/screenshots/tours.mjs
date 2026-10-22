// A tour is a short product video: a sequence of captured screens animated on
// a 1920x1080 stage (frame/stage.html) and encoded to MP4 by video.mjs.
//
// Everything here is data. Times are absolute seconds on the video timeline,
// so the file reads like the storyboard it was written from:
//
//   0-2s   open LaraEnv            (dashboard, window launches)
//   2-5s   PHP 7.4 and 8.4         (projects, two rows highlighted)
//   5-8s   Nginx / Apache / MySQL  (dashboard services, three cards)
//   8-11s  open Terminal           (terminal page)
//   11-14s open SSH                (ssh page)
//
// A scene references a capture by id; captures are taken once each through
// the same harness as the banners (capture.mjs), so every pixel is the real
// frontend against the fake backend. Boxes are measured in the captured page
// and given names the scenes refer to — that is how a highlight lands on a
// row without anyone hand-copying coordinates.

import { getScenario } from "./mock/scenarios.mjs";

// The storyboard names Apache explicitly, and the dashboard renders whatever
// ListServices returns, so the tour swaps the banner's Mailpit for Apache.
const SERVICES = [
  { name: "nginx", label: "Nginx" },
  { name: "apache", label: "Apache" },
  { name: "mysql", label: "MySQL" },
];

// Measured in every capture: the sidebar is the same on every page, and the
// cursor clicks it to move between scenes.
const SIDEBAR = {
  "sidebar.dashboard": (page) => page.getByRole("button", { name: "Dashboard", exact: true }),
  "sidebar.projects": (page) => page.getByRole("button", { name: "Projects", exact: true }),
  "sidebar.terminal": (page) => page.getByRole("button", { name: "Terminal", exact: true }),
  "sidebar.ssh": (page) => page.getByRole("button", { name: "SSH", exact: true }),
};

// Derive from the banner scenarios so the two never drift apart: same data,
// same navigation, same readiness proof.
function fromScenario(id, patch = {}) {
  const s = getScenario(id);
  if (!s) throw new Error(`unknown scenario "${id}"`);
  return { ...s, state: { ...s.state, services: SERVICES, ...patch } };
}

export const CAPTURES = {
  dashboard: {
    ...fromScenario("dashboard"),
    measure: {
      ...SIDEBAR,
      // ServiceCard renders a button.glass with the label inside it.
      "svc.nginx": (page) => page.locator("button.glass", { hasText: "Nginx" }).first(),
      "svc.apache": (page) => page.locator("button.glass", { hasText: "Apache" }).first(),
      "svc.mysql": (page) => page.locator("button.glass", { hasText: "MySQL" }).first(),
    },
  },
  projects: {
    ...fromScenario("projects"),
    measure: {
      ...SIDEBAR,
      // The right-hand "PHP x.y" label of a row is a plain span
      // (Projects.tsx: <span>PHP {phpLabel}</span>). The expanded row's
      // runtime dropdown says the same thing but is a button, so match the
      // tag, and :visible skips the collapsed copies that precede the rows in
      // DOM order. First visible 8.4 is acme-shop, first visible 7.4 is atlas-crm.
      php84: (page) => page.locator('span:text-is("PHP 8.4"):visible').first(),
      php74: (page) => page.locator('span:text-is("PHP 7.4"):visible').first(),
    },
  },
  terminal: {
    ...fromScenario("terminal"),
    measure: {
      ...SIDEBAR,
      term: (page) => page.locator(".xterm:visible").first(),
    },
  },
  ssh: {
    ...fromScenario("ssh"),
    measure: {
      ...SIDEBAR,
      host0: (page) => page.locator("article.ssh-host").first(),
      "host0.connect": (page) => page.locator("button.ssh-connect").first(),
    },
  },
};

// Storyboard. `start` is when the scene's page is on screen; the previous
// scene stays underneath for `fade` seconds so the cut reads as the app
// switching pages, not as a jump.
//
// zoom      eases the window from 1x to `scale`, centred on the union of the
//           named boxes; the runner keeps the frame covered, so the scene
//           ends up cropped rather than floating.
// highlight draws a ring around a box at `at`.
// caption   the lower-left pill.
// cursor    an event the pointer moves to over `move` seconds and, when
//           `click`, presses at `at`. Targets are box names in this scene.
export const TOUR = {
  id: "tour",
  out: "tour.mp4",
  width: 1920,
  height: 1080,
  fps: 30,
  duration: 14,
  fade: 0.3,
  fadeOutAt: 13.5,
  scenes: [
    {
      id: "open",
      capture: "dashboard",
      start: 0,
      // The app icon sits on the stage; a double-click launches the window.
      launch: { icon: [0, 0.6], dblclick: [0.42, 0.56], window: [0.6, 1.25] },
      caption: { text: "Local dev, simplified.", at: 1.05 },
      cursor: [
        { at: 0.42, target: "icon", move: 0.32, click: true },
        { at: 0.56, target: "icon", move: 0, click: true },
        { at: 1.7, target: "sidebar.projects", move: 0.5, click: true },
      ],
    },
    {
      id: "php",
      capture: "projects",
      start: 1.85,
      zoom: { focus: ["php84", "php74"], scale: 1.32, from: 2.0, to: 3.3 },
      highlights: [
        { box: "php84", at: 2.55 },
        { box: "php74", at: 2.9 },
      ],
      caption: { text: "PHP 7.4 and 8.4, per project", at: 2.3 },
      cursor: [{ at: 4.75, target: "sidebar.dashboard", move: 0.5, click: true }],
    },
    {
      id: "services",
      capture: "dashboard",
      start: 4.9,
      zoom: { focus: ["svc.nginx", "svc.mysql"], scale: 1.3, from: 5.05, to: 6.3 },
      highlights: [
        { box: "svc.nginx", at: 5.5 },
        { box: "svc.apache", at: 5.8 },
        { box: "svc.mysql", at: 6.1 },
      ],
      caption: { text: "Nginx · Apache · MySQL", at: 5.4 },
      cursor: [{ at: 7.75, target: "sidebar.terminal", move: 0.5, click: true }],
    },
    {
      id: "terminal",
      capture: "terminal",
      start: 7.9,
      zoom: { focus: ["term"], scale: 1.18, from: 8.05, to: 9.6 },
      caption: { text: "Built-in terminal", at: 8.4 },
      cursor: [{ at: 10.75, target: "sidebar.ssh", move: 0.5, click: true }],
    },
    {
      id: "ssh",
      capture: "ssh",
      start: 10.9,
      zoom: { focus: ["host0"], scale: 1.25, from: 11.05, to: 12.6 },
      highlights: [{ box: "host0", at: 11.9 }],
      caption: { text: "SSH hosts, one click away", at: 11.4 },
      cursor: [{ at: 12.7, target: "host0.connect", move: 0.55, click: false }],
    },
  ],
};

export const TOURS = [TOUR];

export function getTour(id) {
  return TOURS.find((t) => t.id === id);
}

export function totalFrames(tour) {
  return Math.round(tour.duration * tour.fps);
}

// Every problem the stage would otherwise surface as a frame that looks wrong:
// a scene out of order, an event outside its scene, a box nobody measures.
// Returned as strings so a test can print them all at once.
export function validateTour(tour, captures = CAPTURES) {
  const problems = [];
  const scenes = tour.scenes;
  scenes.forEach((sc, i) => {
    const cap = captures[sc.capture];
    if (!cap) { problems.push(`${sc.id}: unknown capture "${sc.capture}"`); return; }
    const end = i + 1 < scenes.length ? scenes[i + 1].start : tour.duration;
    if (i > 0 && !(sc.start > scenes[i - 1].start)) problems.push(`${sc.id}: starts before the previous scene`);
    if (sc.start >= end) problems.push(`${sc.id}: zero-length scene`);

    const known = (name) => name === "icon" || Object.hasOwn(cap.measure, name);
    const inScene = (label, t) => {
      if (t < sc.start || t > end) problems.push(`${sc.id}: ${label} at ${t}s is outside [${sc.start}, ${end}]`);
    };
    for (const name of sc.zoom?.focus ?? []) if (!known(name)) problems.push(`${sc.id}: zoom focus "${name}" is not measured`);
    if (sc.zoom) { inScene("zoom start", sc.zoom.from); inScene("zoom end", sc.zoom.to); }
    for (const h of sc.highlights ?? []) {
      if (!known(h.box)) problems.push(`${sc.id}: highlight "${h.box}" is not measured`);
      inScene(`highlight ${h.box}`, h.at);
    }
    if (sc.caption) inScene("caption", sc.caption.at);
    for (const ev of sc.cursor ?? []) {
      if (!known(ev.target)) problems.push(`${sc.id}: cursor target "${ev.target}" is not measured`);
      inScene(`cursor ${ev.target}`, ev.at);
      inScene(`cursor ${ev.target} move start`, ev.at - ev.move);
    }
    if (sc.launch && i !== 0) problems.push(`${sc.id}: only the first scene can launch the window`);
  });
  if (tour.fadeOutAt >= tour.duration) problems.push("fadeOutAt must be before the end");
  return problems;
}

// AcornFox 网页控制台。
// 不依赖框架，ES2020。所有内容用 createElement + textContent 生成，用户数据
// 永远不进 innerHTML（安全策略：不内联脚本 / 样式、不用 eval、不加载外部资源）。
// 结构：顶栏 + 四个页面（我的应用 / 应用中心 / 服务器 / 设置）+ 应用详情页。

// ---------------------------------------------------------------------------
// 常量与小工具
// ---------------------------------------------------------------------------

const POLL_MS = 3000;
const HOME_TOOLS_MIN = 6;                       // 应用数达到这个值时首页出现工具栏、隐藏“部署新应用”卡片
const HOME_PAGE_SIZE = { grid: 12, list: 15 };  // 卡片 4 行 × 3 列；列表一屏左右

// 应用的显示状态：卡片、标题和提醒共用。
const STATUS = {
  running: { text: "运行中", cls: "running" },
  deploying: { text: "部署中", cls: "deploying" },
  failed: { text: "部署失败", cls: "error" },
  crashed: { text: "未在运行", cls: "error" },
  stopped: { text: "已停止", cls: "stopped" },
};

const PENDING_STATUSES = new Set(["queued", "building", "starting", "checking", "routing"]);

// 部署流程的五步，与诊断的 stage 对应。
const STEPS = ["上传", "构建", "启动", "健康检查", "切换流量"];
const STEP_OF_STATUS = { queued: 0, building: 1, starting: 2, checking: 3, routing: 4 };
const STEP_OF_STAGE = { upload: 0, build: 1, runner: 1, start: 2, addon: 2, health: 3, data: 3, route: 4 };

const PAGES = [
  ["home", "我的应用", "#/"],
  ["store", "应用中心", "#/store"],
  ["server", "服务器", "#/server"],
  ["settings", "设置", "#/settings"],
];

const APP_TABS = [["overview", "概览"], ["logs", "日志"], ["versions", "版本记录"], ["config", "配置"], ["db", "数据库"], ["domains", "域名"]];

const ADDON_NAMES = { postgres: "PostgreSQL", mysql: "MySQL", redis: "Redis" };
const ADDON_KINDS = ["postgres", "mysql", "redis"];
const ADDON_DESC = {
  postgres: "关系型数据库，连接地址注入 DATABASE_URL。",
  mysql: "关系型数据库，连接地址注入 DATABASE_URL。",
  redis: "缓存与会话，连接地址注入 REDIS_URL。",
};

const SKILL_PROMPT = "请安装 AcornFox Skill：下载 https://acornfox.com/skill/SKILL.md ，保存为你 skills 目录下的 acornfox/SKILL.md（Claude Code 是 ~/.claude/skills，Codex 是 ~/.codex/skills），然后按这个 Skill 把当前项目部署到我的服务器。";
const UPGRADE_CN = "curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/scripts/install/upgrade.sh | sudo bash";
const UPGRADE_GLOBAL = "curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/upgrade.sh | sudo bash";

// 应用中心首批预告（v0.5 开放前只展示，不提供安装）。[名称, 一句话, 分类]
const STORE_PREVIEW = [
  ["Hermes 智能体", "常驻的 AI 智能体，可以接飞书、钉钉、企业微信做机器人。", "AI 智能体 · 开源"],
  ["New API", "把多家大模型合成一个接口，统一管理 Key 和额度。", "模型网关 · 开源"],
];

// el 创建元素：文字一律作为文本节点，绝不经过 innerHTML。
function el(tag, attrs, children) {
  const node = document.createElement(tag);
  if (attrs) {
    for (const [k, v] of Object.entries(attrs)) {
      if (v === undefined || v === null || v === false) continue;
      if (k === "class") node.className = v;
      else if (k === "text") node.textContent = v;
      else if (k === "dataset") { for (const [dk, dv] of Object.entries(v)) node.dataset[dk] = dv; }
      else if (k.startsWith("on") && typeof v === "function") node.addEventListener(k.slice(2), v);
      else node.setAttribute(k, v);
    }
  }
  if (children) for (const c of [].concat(children)) { if (c != null) node.append(c); }
  return node;
}

function text(s) { return document.createTextNode(s); }

function clear(node) { while (node.firstChild) node.removeChild(node.firstChild); }

// appIcon 是首字母图标，按名字固定配一种颜色（8 种，避开红橙黄，深色模式另有一套），
// 首字母相同的应用也能分开。
function appIcon(name, extra) {
  let h = 0;
  for (const c of name || "") h = (h * 31 + c.charCodeAt(0)) >>> 0;
  return el("span", { class: "app-icon" + (extra ? " " + extra : ""), "aria-hidden": "true", dataset: { hue: String(h % 8) }, text: firstChar(name) });
}

// firstChar 取名字的第一个字，用作应用图标。
function firstChar(name) {
  const chars = Array.from(name || "?");
  return chars.length ? chars[0].toUpperCase() : "?";
}

// timeAgo 把时间戳显示成“N 分钟前”。
function timeAgo(iso) {
  if (!iso) return "";
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return "";
  const s = Math.max(0, Math.floor((Date.now() - t) / 1000));
  if (s < 60) return "刚刚";
  const m = Math.floor(s / 60);
  if (m < 60) return m + " 分钟前";
  const h = Math.floor(m / 60);
  if (h < 24) return h + " 小时前";
  return Math.floor(h / 24) + " 天前";
}

// tookText 显示一次部署的用时。
function tookText(d) {
  if (!d || !d.created_at || !d.finished_at) return "";
  const s = Math.round((Date.parse(d.finished_at) - Date.parse(d.created_at)) / 1000);
  if (!Number.isFinite(s) || s < 0) return "";
  return s < 60 ? `用时 ${s} 秒` : `用时 ${Math.floor(s / 60)} 分 ${s % 60} 秒`;
}

function pct(used, total) { return total ? Math.round((used / total) * 100) : 0; }

// ---------------------------------------------------------------------------
// 接口（CSRF + 错误分类）
// ---------------------------------------------------------------------------

const api = {
  csrf: null,

  async session() {
    const r = await fetch("/v1/console/session", { headers: { Accept: "application/json" } });
    if (!r.ok) throw await apiError(r);
    const body = await r.json();
    this.csrf = body.csrf || null;
    return body;
  },

  async get(path, signal) {
    const r = await fetch(path, { headers: { Accept: "application/json" }, signal });
    if (!r.ok) throw await apiError(r);
    return r.json();
  },

  async send(method, path, body) {
    const headers = { "Content-Type": "application/json" };
    if (this.csrf) headers["X-AcornFox-CSRF"] = this.csrf;
    const r = await fetch(path, { method, headers, body: body ? JSON.stringify(body) : null });
    if (!r.ok) throw await apiError(r);
    return r.status === 204 ? {} : r.json().catch(() => ({}));
  },

  post(path, body) { return this.send("POST", path, body); },
  del(path) { return this.send("DELETE", path, null); },
};

async function apiError(r) {
  let code = "server_error", message = "服务器错误";
  try {
    const body = await r.json();
    if (body && body.error) { code = body.error.code || code; message = body.error.message || message; }
    else if (body && body.code) { code = body.code; message = body.message || message; }
  } catch (_) { /* 非 JSON */ }
  const e = new Error(message);
  e.status = r.status;
  e.code = code;
  return e;
}

function appPath(name, rest) { return "/v1/apps/" + encodeURIComponent(name) + (rest || ""); }

// ---------------------------------------------------------------------------
// 应用状态推导
// ---------------------------------------------------------------------------

// deriveStatus：期望停止 → 已停止；最新部署进行中 → 部署中；最新部署失败 →
// 部署失败（旧版本可能仍在服务）；容器在跑 → 运行中；否则未在运行。
function deriveStatus(app) {
  if (app.desired === "stopped") return "stopped";
  const latest = app.latest || app.live;
  if (latest && PENDING_STATUSES.has(latest.status)) return "deploying";
  if (latest && latest.status === "failed") return "failed";
  if (app.observed_state === "running") return "running";
  if (app.observed_state === "stopped" || app.observed_state === "missing") return "crashed";
  return "running";
}

// oldVersionServing：最新一次部署没上线，但之前的版本仍在服务。
function oldVersionServing(app) {
  const latest = app.latest;
  if (!latest || latest.status !== "failed") return false;
  if (app.live) return app.live.id !== latest.id && app.live.status === "live";
  return !!app.current_deployment && app.current_deployment !== latest.id;
}

function isRunning(app) {
  if (app.observed) return !!app.observed.running;
  return app.observed_state === "running";
}

function appDiagnosis(app) {
  const latest = app.latest || app.live;
  if (latest && latest.status === "failed" && latest.diagnosis) return latest.diagnosis;
  return null;
}

function failedDeploymentID(app) {
  const latest = app.latest || app.live;
  return latest ? latest.id : "";
}

function domainDiagnosis(app) {
  for (const d of app.domains || []) {
    if (d.status === "failed" && d.diagnosis) return { domain: d.name, diagnosis: d.diagnosis };
  }
  return null;
}

// copyForAI 生成交给 AI 的诊断文本（格式与 CLI 一致）。
function copyForAI(diag, app, deploymentID) {
  const payload = {
    stage: diag.stage,
    code: diag.code,
    message: diag.message,
    log_excerpt: diag.log_excerpt || "",
    hint: diag.hint || "",
    app: app,
    deployment_id: deploymentID || "",
  };
  return "请根据以下 AcornFox 部署诊断修复项目，然后重新执行 acornfox deploy：\n" + JSON.stringify(payload, null, 2);
}

// statusForAI 生成应用当前状态的摘要，供“复制状态给 AI”。
function statusForAI(a) {
  const st = STATUS[deriveStatus(a)].text;
  const lines = ["这是我在 AcornFox 上的应用「" + a.name + "」的当前状态，请帮我看看：", "状态：" + st];
  if (a.live) lines.push("当前版本：第 " + a.live.seq + " 版（" + deploymentSource(a.live) + "）");
  if (a.latest && a.live && a.latest.id !== a.live.id) lines.push("最新一次部署：第 " + a.latest.seq + " 版，" + deployStatusText(a.latest.status));
  if (a.url) lines.push("访问地址：" + a.url);
  if (a.lan_url) lines.push("局域网地址：" + a.lan_url);
  const addons = (a.addons || []).map((x) => (ADDON_NAMES[x.kind] || x.kind) + "（" + addonStateText(x.observed_state) + "）");
  if (addons.length) lines.push("数据库：" + addons.join("、"));
  lines.push("资源上限：内存 " + a.memory_mb + " MB，CPU " + (a.cpu_milli / 1000).toFixed(1) + " 核");
  const diag = appDiagnosis(a);
  if (diag) lines.push("", copyForAI(diag, a.name, failedDeploymentID(a)));
  return lines.join("\n");
}

function sourceLabel(kind) {
  return kind === "image" ? "镜像" : kind === "git" ? "Git" : "上传";
}

// reasonText 说明复用镜像的部署为什么产生，如“添加 PostgreSQL（基于第 1 版）”。
function reasonText(d) {
  if (!d || !d.reason) return "";
  const [kind, addon] = d.reason.split(":");
  const what = ({
    redeploy: "重新部署",
    rollback: "回退",
    addon_add: "添加 " + (ADDON_NAMES[addon] || addon || "数据库"),
    addon_remove: "移除 " + (ADDON_NAMES[addon] || addon || "数据库"),
  })[kind] || d.reason;
  return d.based_on_seq ? `${what}（基于第 ${d.based_on_seq} 版）` : what;
}

// deploymentSource 显示代码的原始来源；重新部署一个上传的目录仍是“上传”。
function deploymentSource(d) {
  return sourceLabel(d.origin_kind || d.source_kind);
}

function deployStatusText(status) {
  return ({
    live: "正在服务", retired: "已被新版本替换", failed: "部署失败",
    superseded: "已被更新的提交取代", queued: "排队中", building: "构建中",
    starting: "启动中", checking: "健康检查中", routing: "切换流量中",
  })[status] || status;
}

function addonStateText(s) {
  return ({ running: "运行中", starting: "初始化中", unhealthy: "未就绪", stopped: "已停止", missing: "等待创建", unavailable: "无法读取状态" })[s] || "状态未知";
}

// ---------------------------------------------------------------------------
// 全局状态
// ---------------------------------------------------------------------------

const state = {
  apps: [],
  host: null,
  server: "",
  route: { page: "home" },
  openApp: null,       // 当前打开的应用名（应用详情页）
  detailTab: "overview",
  detail: null,
  logs: null,
  logCursor: "", logFollow: false, logTail: 100, logError: null,
  logGeneration: 0, appMetrics: null,
  deployments: null,
  fatal: null,
  connectionLost: false,
  loaded: false,
  busy: {},            // 正在提交的操作，避免重复点击
  resources: null,     // GET /v1/resources：整机上限与各容器已分配
  focusLimits: false,  // 从概览“调整上限”进入配置标签时，把资源上限卡片滚到视口中间
  // 多应用首页（应用数 ≥ HOME_TOOLS_MIN 时出现工具栏）。筛选、搜索、页码只在本页有效，视图记在浏览器里。
  homeQuery: "", homeFilter: "all", homeSort: "attn", homePage: 1, homeView: loadHomeView(),
  limitDraft: null,    // 正在拖动、还没保存的上限：{ key: "app:名字" | "host", memory, cpu }
};

let pollTimer = null;
let logRequest = null;
let pollInFlight = false;

const $ = (id) => document.getElementById(id);
const FOCUSABLE = "button, a, summary, select, input, [tabindex]";

// ---------------------------------------------------------------------------
// 通用组件
// ---------------------------------------------------------------------------

function button(label, cls, onClick, ariaLabel, children) {
  const attrs = { class: cls, type: "button", onclick: onClick };
  if (ariaLabel) attrs["aria-label"] = ariaLabel;
  const b = el("button", attrs, children || null);
  if (label != null) b.append(text(label));
  return b;
}

function linkButton(label, cls, href, extra) {
  return el("a", Object.assign({ class: cls, href }, extra || {}), text(label));
}

// switchNode 是“一拨即开”开关：role=switch，开启时轨道内显示三个圆点。
function switchNode(checked, label, onToggle, opts) {
  const o = opts || {};
  return el("button", {
    class: "switch" + (o.sm ? " sm" : ""), type: "button", role: "switch",
    "aria-checked": checked ? "true" : "false", "aria-label": label,
    disabled: o.disabled ? "" : false, onclick: onToggle,
  }, [
    el("span", { class: "dots", "aria-hidden": "true" }, [el("span"), el("span"), el("span")]),
    el("span", { class: "knob", "aria-hidden": "true" }),
  ]);
}

function badge(cls, label) {
  return el("span", { class: "badge " + cls }, [el("span", { class: "dot " + cls, "aria-hidden": "true" }), text(label)]);
}

function statusBadge(st, label) {
  return badge(STATUS[st].cls, label || STATUS[st].text);
}

function card(children, cls, region) {
  return el("section", { class: "card" + (cls ? " " + cls : ""), dataset: region ? { region } : undefined }, children);
}

function cardHead(title, side) {
  return el("div", { class: "card-head" }, [el("h2", { class: "section-title", text: title }), side || null]);
}

function copyBlock(content, okMsg, label, mono) {
  return el("div", { class: "codebox" }, [
    el("span", { class: mono ? "mono" : "", text: content }),
    el("div", {}, button("复制", "btn sm", () => copyToClipboard(content, okMsg), label)),
  ]);
}

function progressNode(currentStep, failedStep, compact) {
  const ol = el("ol", { class: "progress" + (compact ? " compact" : ""), "aria-label": "部署进度" });
  STEPS.forEach((name, i) => {
    let cls = "";
    let label = name;
    if (failedStep != null) {
      if (i < failedStep) { cls = "done"; label = name + " ✓"; }
      else if (i === failedStep) { cls = "fail"; label = name + " ✕"; }
    } else if (currentStep != null) {
      if (i < currentStep) cls = "done";
      else if (i === currentStep) cls = "now";
    }
    ol.append(el("li", { class: cls, text: label }));
  });
  return ol;
}

function logoSVG() {
  const ns = "http://www.w3.org/2000/svg";
  const svg = document.createElementNS(ns, "svg");
  svg.setAttribute("viewBox", "0 0 120 60");
  svg.setAttribute("aria-hidden", "true");
  const p = document.createElementNS(ns, "path");
  p.setAttribute("class", "logo-track");
  p.setAttribute("fill-rule", "evenodd");
  p.setAttribute("d", "M30 0 H90 A30 30 0 0 1 90 60 H30 A30 30 0 0 1 30 0 Z M68 30 A22 22 0 1 0 112 30 A22 22 0 1 0 68 30 Z M16 30 A3.5 3.5 0 1 0 23 30 A3.5 3.5 0 1 0 16 30 Z M30.3 30 A5 5 0 1 0 40.3 30 A5 5 0 1 0 30.3 30 Z M47.7 30 A6.5 6.5 0 1 0 60.7 30 A6.5 6.5 0 1 0 47.7 30 Z");
  const c = document.createElementNS(ns, "circle");
  c.setAttribute("class", "logo-knob");
  c.setAttribute("cx", "90"); c.setAttribute("cy", "30"); c.setAttribute("r", "22");
  svg.append(p, c);
  return svg;
}

// 底部标签栏的四个小图标。
function tabIcon(id) {
  const ns = "http://www.w3.org/2000/svg";
  const svg = document.createElementNS(ns, "svg");
  svg.setAttribute("viewBox", "0 0 24 24");
  svg.setAttribute("aria-hidden", "true");
  const add = (tag, attrs) => {
    const n = document.createElementNS(ns, tag);
    for (const [k, v] of Object.entries(attrs)) n.setAttribute(k, v);
    n.setAttribute("fill", attrs.fill || "none");
    if (!attrs.fill) { n.setAttribute("stroke", "currentColor"); n.setAttribute("stroke-width", "1.7"); }
    svg.append(n);
  };
  if (id === "home") { add("rect", { x: "2.5", y: "7", width: "19", height: "10", rx: "5" }); add("circle", { cx: "16.5", cy: "12", r: "3", fill: "currentColor" }); }
  else if (id === "store") { add("rect", { x: "4", y: "4", width: "7", height: "7", rx: "2" }); add("rect", { x: "13", y: "4", width: "7", height: "7", rx: "2" }); add("rect", { x: "4", y: "13", width: "7", height: "7", rx: "2" }); add("rect", { x: "13", y: "13", width: "7", height: "7", rx: "3.5" }); }
  else if (id === "server") { add("rect", { x: "4", y: "4", width: "16", height: "6.5", rx: "2" }); add("rect", { x: "4", y: "13.5", width: "16", height: "6.5", rx: "2" }); add("circle", { cx: "8", cy: "7.25", r: "1", fill: "currentColor" }); add("circle", { cx: "8", cy: "16.75", r: "1", fill: "currentColor" }); }
  else { add("path", { d: "M4 7h16M4 17h16" }); add("circle", { cx: "9", cy: "7", r: "2.4" }); add("circle", { cx: "15", cy: "17", r: "2.4" }); }
  return svg;
}

// ---------------------------------------------------------------------------
// 渲染：只替换有变化的区域，轮询不打断选择、焦点和滚动
// ---------------------------------------------------------------------------

function focusKey(node) {
  return node.tagName + ":" + (node.getAttribute("aria-label") || node.getAttribute("href") || node.id || node.textContent);
}

function selectionInside(node) {
  const sel = window.getSelection();
  if (!node || !sel || sel.isCollapsed || !sel.rangeCount) return false;
  return node.contains(sel.anchorNode) || node.contains(sel.focusNode);
}

// 滑块由 syncSliders 定位，不参与内容比较，也不随内容替换，这样才能从旧位置滑到新位置。
function isDecor(n) { return n.nodeType === 1 && n.classList.contains("slider"); }
function contentNodes(node) { return Array.from(node.childNodes).filter((n) => !isDecor(n)); }

function sameChildren(a, b) {
  const cur = contentNodes(a);
  if (cur.length !== b.childNodes.length) return false;
  for (let i = 0; i < cur.length; i++) {
    if (!cur[i].isEqualNode(b.childNodes[i])) return false;
  }
  return true;
}

function swapChildren(target, next) {
  if (sameChildren(target, next)) return false;
  for (const n of contentNodes(target)) target.removeChild(n);
  while (next.firstChild) target.append(next.firstChild);
  return true;
}

function regionKey(node) {
  return node.nodeType === 1 ? node.nodeName + "|" + (node.dataset.region || "") : "#" + node.nodeType;
}

// patch 把 next 的内容合到 target：结构相同时逐个区域比较，只替换变化的；
// 日志框只更新文字并保留滚动位置；用户正在选择的区域本轮不动。
function patch(target, next) {
  const a = contentNodes(target), b = Array.from(next.childNodes);
  const sameShape = a.length === b.length && a.every((n, i) => regionKey(n) === regionKey(b[i]));
  if (!sameShape) { swapChildren(target, next); return; }
  for (let i = 0; i < a.length; i++) {
    const cur = a[i], nxt = b[i];
    if (cur.isEqualNode(nxt)) continue;
    if (cur.nodeType === 1 && cur.classList.contains("logbox") && nxt.classList.contains("logbox")) {
      if (selectionInside(cur)) continue;
      const atBottom = cur.scrollHeight - cur.scrollTop - cur.clientHeight < 24;
      const top = cur.scrollTop;
      cur.textContent = nxt.textContent;
      cur.scrollTop = state.logFollow && atBottom ? cur.scrollHeight : top;
      continue;
    }
    if (cur.nodeType === 1 && selectionInside(cur)) continue;
    // 正在拖动上限滑块的卡片本轮不动，等保存或取消后再刷新
    if (cur.nodeType === 1 && cur.dataset.editing === "1" && nxt.nodeType === 1 && nxt.dataset.editing === "1") continue;
    // 整张替换的卡片：里面的滑块和按钮引用的是同一次生成的卡片，只换一部分会让它们指向页面上不存在的那张
    if (cur.nodeType === 1 && cur.dataset.atomic === "1") { cur.replaceWith(nxt); continue; }
    if (cur.nodeType === 1 && cur.dataset.region && cur.nodeName === nxt.nodeName) {
      patch(cur, nxt);
      for (const attr of ["class", "aria-label", "aria-checked", "aria-selected"]) {
        let v = nxt.getAttribute(attr);
        // 切换动效播放期间数据回来重绘，保留 playEnter 加的 class，动画不被打断
        if (attr === "class") for (const c of ["enter-r", "enter-l"]) if (cur.classList.contains(c)) v = (v ? v + " " : "") + c;
        if (v == null) cur.removeAttribute(attr); else if (cur.getAttribute(attr) !== v) cur.setAttribute(attr, v);
      }
      continue;
    }
    cur.replaceWith(nxt);
  }
}

function render() {
  const active = document.activeElement;
  const activeKey = active && active.matches && active.matches(FOCUSABLE) ? focusKey(active) : null;
  const scope = $("page");
  const activeIndex = activeKey ? Array.from(document.querySelectorAll(FOCUSABLE)).filter((n) => focusKey(n) === activeKey).indexOf(active) : 0;
  renderFatal();
  if (state.fatal) return;
  renderConnectionBanner();
  renderEdgeBanner();
  renderSetupLayer();
  renderChrome();
  const next = document.createElement("div");
  buildPage(next);
  patch(scope, next);
  scope.classList.toggle("is-home", state.loaded && state.route.page === "home" && state.apps.length > 0);
  syncSliders();
  if (activeKey && !active.isConnected) {
    const replacement = Array.from(document.querySelectorAll(FOCUSABLE)).filter((n) => focusKey(n) === activeKey)[activeIndex];
    if (replacement) replacement.focus({ preventScroll: true });
  }
}

function renderFatal() {
  const existing = $("fatal-state");
  if (!state.fatal) { if (existing) existing.remove(); return; }
  const { title, body, again } = state.fatal;
  const box = el("div", { class: "card" }, [
    el("h2", { class: "section-title", text: title }),
    el("p", { class: "muted", text: body }),
    again ? el("p", { class: "faint", text: "请在你的电脑终端重新执行 acornfox open。" }) : null,
  ]);
  if (existing) { clear(existing); existing.append(box); return; }
  document.body.append(el("div", { id: "fatal-state", class: "state" }, box));
}

function renderConnectionBanner() {
  const existing = $("conn-banner");
  if (!state.connectionLost || state.fatal) { if (existing) existing.remove(); return; }
  if (existing) return;
  document.body.append(el("div", { id: "conn-banner", class: "banner", role: "status" }, el("div", { class: "inner", text: "与服务器的连接中断，正在重试…" })));
}

// 网关组件（Caddy 容器）还没就绪时提示：首次启动要下载镜像，这期间应用网址打不开。
function renderEdgeBanner() {
  const existing = $("edge-banner");
  const e = state.edge;
  const show = e && (e.state === "preparing" || e.state === "error") && !state.connectionLost && !state.fatal && !setupVisible() && !(state.setup && state.setup.state !== "done");
  if (!show) { if (existing) existing.remove(); return; }
  const text = e.state === "preparing"
    ? "正在准备网关组件（首次启动要下载 Caddy 镜像，约 50 MB），完成前应用网址暂时打不开。"
    : "网关组件（Caddy）启动失败，正在重试：" + (e.message || "") + "。可在终端运行 acornfox doctor 查看。";
  if (existing && existing.dataset.text === text) return;
  if (existing) existing.remove();
  document.body.append(el("div", { id: "edge-banner", class: "banner", role: "status", dataset: { text } }, el("div", { class: "inner", text })));
}

function currentPage() {
  return state.route.page === "app" || state.route.page === "new" ? "home" : state.route.page;
}

function renderChrome() {
  const label = $("server-label");
  const serverText = state.server || (state.loaded ? "服务器" : "正在连接…");
  if (label.textContent !== serverText) label.textContent = serverText;
  // 服务器标签只表示连接状态；需要处理的问题统一交给铃铛
  const dot = $("server-dot");
  const dotCls = !state.loaded || state.connectionLost ? "dot stopped" : "dot";
  if (dot.className !== dotCls) dot.className = dotCls;
  $("server-chip").setAttribute("title", !state.loaded ? "正在连接服务器" : state.connectionLost ? "与服务器的连接中断" : "已连接服务器");
  renderBell();

  const page = currentPage();
  const nav = el("div");
  const bar = el("div");
  for (const [id, name, href] of PAGES) {
    nav.append(el("a", { href, "aria-current": id === page ? "page" : false, text: name }));
    bar.append(el("a", { href, "aria-current": id === page ? "page" : false }, [tabIcon(id), text(name)]));
  }
  swapChildren($("nav"), nav);
  swapChildren($("tabbar"), bar);
}

function buildPage(page) {
  if (!state.loaded) {
    page.append(el("div", { class: "state", dataset: { region: "loading" } }, el("div", { class: "card" }, [
      el("div", { class: "spinner", "aria-hidden": "true" }), el("p", { text: "正在连接服务器…" }),
    ])));
    return;
  }
  const r = state.route.page;
  if (r === "store") buildStore(page);
  else if (r === "server") buildServer(page);
  else if (r === "settings") buildSettings(page);
  else if (r === "new") buildStart(page, false);
  else if (r === "app") buildApp(page);
  else buildHome(page);
}

// ---------------------------------------------------------------------------
// 我的应用
// ---------------------------------------------------------------------------

function buildHome(page) {
  if (state.apps.length === 0) { buildStart(page, true); return; }
  const running = state.apps.filter((a) => deriveStatus(a) === "running").length;
  const items = attentionItems();
  const sub = el("p", { class: "sub" }, text(`${state.apps.length} 个应用 · ${running} 个运行中`));
  // 需要处理的问题在顶栏铃铛里；这里的数字点一下也能打开铃铛面板
  if (items.length) sub.append(text(" · "), el("button", { type: "button", class: "sub-alert", onclick: (e) => { e.stopPropagation(); openNotify(); } }, text(`${items.length} 项需要处理`)));
  page.append(el("div", { class: "page-head", dataset: { region: "head" } }, [
    el("div", {}, [el("h1", { text: "我的应用" }), sub]),
    linkButton("新建应用", "btn primary knob", "#/new"),
  ]));
  // 应用少时保持原样：卡片网格加“部署新应用”卡片；多了才出现工具栏、列表视图和翻页。
  const tools = state.apps.length >= HOME_TOOLS_MIN;
  const view = tools ? state.homeView : "grid";
  const list = tools ? homeFilteredApps() : state.apps;
  if (tools) page.append(homeToolbar(view));
  const size = HOME_PAGE_SIZE[view];
  const pages = tools ? Math.max(1, Math.ceil(list.length / size)) : 1;
  if (state.homePage > pages) state.homePage = pages;
  const shown = tools ? list.slice((state.homePage - 1) * size, state.homePage * size) : list;
  if (!list.length) {
    page.append(el("div", { class: "home-empty", dataset: { region: "empty" } }, [
      el("span", { text: state.homeQuery ? `没有匹配「${state.homeQuery}」的应用` : "这个筛选下没有应用" }),
      button("清除筛选", "btn sm", () => { state.homeQuery = ""; state.homeFilter = "all"; state.homePage = 1; render(); }, "清除搜索和筛选"),
    ]));
  } else if (view === "list") {
    page.append(appList(shown));
  } else {
    const grid = el("div", { class: "apps", dataset: { region: "apps" } });
    for (const a of shown) grid.append(appCard(a));
    if (!tools) grid.append(el("a", { class: "add-card", href: "#/new", "aria-label": "部署新应用" }, [
      el("b", { text: "＋ 部署新应用" }),
      el("span", { class: "faint", text: "让 AI 部署你的项目，或用命令行部署目录、Git 仓库、镜像。" }),
    ]));
    page.append(grid);
  }
  if (tools && list.length) page.append(homePager(list.length, pages, size));
  page.append(ambientLine());
  page.append(el("div", { class: "home-decor", "aria-hidden": "true", dataset: { region: "decor" } }));
}

// ---------------------------------------------------------------------------
// 多应用首页：搜索、状态筛选、排序、卡片/列表视图、翻页。全部在浏览器里完成，不改接口。
// ---------------------------------------------------------------------------

const HOME_FILTERS = [["all", "全部"], ["running", "运行中"], ["stopped", "已停止"], ["deploying", "部署中"], ["attention", "需处理"]];
const HOME_SORTS = [["attn", "需处理优先"], ["recent", "最近部署"], ["name", "名称"], ["mem", "内存从高到低"]];
const HOME_RANK = { failed: 0, crashed: 0, deploying: 1, running: 2, stopped: 3 };

function loadHomeView() {
  try {
    const v = localStorage.getItem("af-home-view");
    if (v === "grid" || v === "list") return v;
  } catch (_) { /* 无痕模式等读不到时用默认 */ }
  // 手机宽度第一次打开默认列表，一屏能看到更多应用
  return typeof window !== "undefined" && window.matchMedia && window.matchMedia("(max-width: 760px)").matches ? "list" : "grid";
}

function homeFilterKey(a) {
  const st = deriveStatus(a);
  return st === "failed" || st === "crashed" ? "attention" : st;
}

function lastChange(a) {
  const d = a.latest || a.live;
  return d ? Date.parse(d.updated_at || d.created_at) || 0 : 0;
}

function homeFilteredApps() {
  const q = state.homeQuery.trim().toLowerCase();
  const list = state.apps.filter((a) =>
    (state.homeFilter === "all" || homeFilterKey(a) === state.homeFilter) &&
    (!q || a.name.toLowerCase().includes(q) || String(a.public_port || "").includes(q)));
  const by = {
    attn: (a, b) => HOME_RANK[deriveStatus(a)] - HOME_RANK[deriveStatus(b)] || lastChange(b) - lastChange(a),
    recent: (a, b) => lastChange(b) - lastChange(a),
    name: (a, b) => a.name.localeCompare(b.name),
    mem: (a, b) => (b.memory_mb || 0) - (a.memory_mb || 0) || a.name.localeCompare(b.name),
  }[state.homeSort] || (() => 0);
  return list.sort(by);
}

function viewIcon(kind) {
  const ns = "http://www.w3.org/2000/svg";
  const svg = document.createElementNS(ns, "svg");
  svg.setAttribute("viewBox", "0 0 16 16");
  svg.setAttribute("aria-hidden", "true");
  const shapes = kind === "grid"
    ? [["rect", { x: "2.5", y: "2.5", width: "4.5", height: "4.5", rx: "1.2" }], ["rect", { x: "9", y: "2.5", width: "4.5", height: "4.5", rx: "1.2" }], ["rect", { x: "2.5", y: "9", width: "4.5", height: "4.5", rx: "1.2" }], ["rect", { x: "9", y: "9", width: "4.5", height: "4.5", rx: "1.2" }]]
    : [["path", { d: "M3 4h10M3 8h10M3 12h10" }]];
  for (const [tag, attrs] of shapes) {
    const n = document.createElementNS(ns, tag);
    for (const [k, v] of Object.entries(attrs)) n.setAttribute(k, v);
    n.setAttribute("fill", "none"); n.setAttribute("stroke", "currentColor"); n.setAttribute("stroke-width", "1.4"); n.setAttribute("stroke-linecap", "round");
    svg.append(n);
  }
  return svg;
}

function homeToolbar(view) {
  // 搜索框的值用属性（property）设置而不写进 HTML 属性：patch 只比较 HTML 属性，
  // 这样输入时搜索框节点保持不变，光标和输入法都不受重绘影响。
  const input = el("input", { type: "search", class: "home-search", placeholder: "搜索应用名或端口", "aria-label": "搜索应用", autocomplete: "off" });
  input.value = state.homeQuery;
  input.addEventListener("input", () => { state.homeQuery = input.value; state.homePage = 1; render(); });
  const counts = { all: state.apps.length };
  for (const a of state.apps) { const k = homeFilterKey(a); counts[k] = (counts[k] || 0) + 1; }
  const chips = el("div", { class: "home-chips", role: "group", "aria-label": "按状态筛选" }, HOME_FILTERS.map(([k, label]) =>
    el("button", {
      type: "button", class: "chip" + (k === "attention" ? " warn" : ""), "aria-pressed": state.homeFilter === k ? "true" : "false",
      onclick: () => { state.homeFilter = k; state.homePage = 1; render(); },
    }, [text(label), el("b", { text: String(counts[k] || 0) })])));
  const sort = el("select", { class: "home-sort", "aria-label": "排序" }, HOME_SORTS.map(([k, label]) => el("option", { value: k, text: label })));
  sort.value = state.homeSort;
  sort.addEventListener("change", () => { state.homeSort = sort.value; state.homePage = 1; render(); });
  const setView = (v) => () => {
    state.homeView = v; state.homePage = 1;
    try { localStorage.setItem("af-home-view", v); } catch (_) { /* 记不住也能用 */ }
    render();
  };
  const views = el("div", { class: "view-seg", role: "group", "aria-label": "视图" }, [
    el("button", { type: "button", "aria-label": "卡片视图", "aria-pressed": view === "grid" ? "true" : "false", onclick: setView("grid") }, viewIcon("grid")),
    el("button", { type: "button", "aria-label": "列表视图", "aria-pressed": view === "list" ? "true" : "false", onclick: setView("list") }, viewIcon("list")),
  ]);
  return el("div", { class: "home-tools", dataset: { region: "tools" } }, [
    el("label", { class: "home-search-wrap" }, [el("span", { class: "sr-only", text: "搜索应用" }), input]),
    chips, sort, views,
  ]);
}

function appList(apps) {
  const box = el("div", { class: "app-list", role: "list", dataset: { region: "app-list" } });
  box.append(el("div", { class: "app-row head", "aria-hidden": "true" }, ["", "应用", "状态", "访问地址", "内存上限", "运行"].map((t) => el("span", { text: t }))));
  for (const a of apps) {
    const st = deriveStatus(a);
    const live = isRunning(a) && st !== "stopped";
    const on = a.desired !== "stopped";
    box.append(el("div", { class: "app-row", role: "listitem", dataset: { region: "row:" + a.name } }, [
      appIcon(a.name),
      el("div", { class: "app-title" }, [
        el("a", { class: "app-name", href: appHash(a.name), text: a.name }),
        el("span", { class: "app-sub", text: appSubLine(a) }),
      ]),
      el("span", { class: "cell-status" }, statusBadge(st)),
      a.url && live
        ? el("a", { class: "cell-url mono", href: a.url, target: "_blank", rel: "noopener", text: a.url.replace(/^https?:\/\//, "") })
        : el("span", { class: "cell-url mono off", text: a.url ? "未运行" : "—" }),
      el("span", { class: "cell-mem mono", text: mbText(a.memory_mb > 0 ? a.memory_mb : 256) }),
      switchNode(on, a.name + " 运行开关", () => toggleRun(a.name, on), { sm: true, disabled: st === "deploying" || !!state.busy[a.name] }),
    ]));
  }
  return box;
}

// pageList 页数多时折叠成“1 … 4 5 6 … 12”。
function pageList(page, pages) {
  if (pages <= 7) return Array.from({ length: pages }, (_, i) => i + 1);
  const out = [1];
  const from = Math.max(2, page - 1), to = Math.min(pages - 1, page + 1);
  if (from > 2) out.push("…");
  for (let i = from; i <= to; i++) out.push(i);
  if (to < pages - 1) out.push("…");
  out.push(pages);
  return out;
}

function homePager(total, pages, size) {
  const page = state.homePage;
  const nav = el("nav", { class: "pager", "aria-label": "翻页", dataset: { region: "pager" } });
  if (pages <= 1) { nav.append(el("span", { class: "pager-info", text: `共 ${total} 个` })); return nav; }
  const go = (p) => () => {
    state.homePage = p;
    render();
    const top = document.querySelector(".home-tools");
    if (top) window.scrollTo({ top: Math.max(0, top.getBoundingClientRect().top + window.scrollY - 64), behavior: reducedMotion() ? "auto" : "smooth" });
  };
  const from = (page - 1) * size + 1, to = Math.min(page * size, total);
  const btns = el("div", { class: "pager-btns" });
  btns.append(el("button", { type: "button", class: "pg", "aria-label": "上一页", disabled: page === 1, onclick: go(page - 1) }, el("span", { "aria-hidden": "true", text: "‹" })));
  for (const p of pageList(page, pages)) {
    if (p === "…") { btns.append(el("span", { class: "pg gap", "aria-hidden": "true", text: "…" })); continue; }
    btns.append(el("button", { type: "button", class: "pg", "aria-label": `第 ${p} 页`, "aria-current": p === page ? "page" : false, onclick: go(p) }, text(String(p))));
  }
  btns.append(el("button", { type: "button", class: "pg", "aria-label": "下一页", disabled: page === pages, onclick: go(page + 1) }, el("span", { "aria-hidden": "true", text: "›" })));
  nav.append(el("span", { class: "pager-info", text: `第 ${from}–${to} 个，共 ${total} 个` }), btns);
  return nav;
}

// ---------------------------------------------------------------------------
// 铃铛：需要处理的问题。顶栏铃铛显示数量，点开是问题面板；出现没提醒过的新问题时
// 铃铛轻晃一下，右上角弹出小卡片 6 秒。“提醒过”记在浏览器里，下次打开不再弹。
// ---------------------------------------------------------------------------

const SEEN_KEY = "af-seen-alerts";
let popTimer = null;

function alertKey(it) { return [it.app, it.deploymentID || "", (it.diag && it.diag.code) || ""].join("|"); }

function loadSeen() {
  try { return new Set(JSON.parse(localStorage.getItem(SEEN_KEY) || "[]")); } catch (_) { return new Set(); }
}

function saveSeen(set) {
  try { localStorage.setItem(SEEN_KEY, JSON.stringify(Array.from(set).slice(-100))); } catch (_) { /* 记不住就每次都提醒 */ }
}

function renderBell() {
  const n = state.loaded ? attentionItems().length : 0;
  const count = $("bell-count");
  const label = n ? `需要处理 ${n} 项` : "需要处理：没有问题";
  if (count.textContent !== String(n)) count.textContent = String(n);
  count.hidden = n === 0;
  if ($("bell").getAttribute("aria-label") !== label) $("bell").setAttribute("aria-label", label);
  if (layerOpen($("notify"))) swapChildren($("notify"), notifyContent());
}

function alertItemNode(it, onAct) {
  const forAI = copyForAI(it.diag, it.app, it.deploymentID);
  return el("div", { class: "notify-item" }, [
    appIcon(it.app),
    el("div", { class: "notify-text" }, [
      el("b", { text: it.title }),
      el("span", { text: it.why }),
      el("div", { class: "row" }, [
        linkButton("查看", "btn sm", appHash(it.app), { "aria-label": "查看 " + it.app, onclick: onAct }),
        button("复制给 AI", "btn sm", () => { copyToClipboard(forAI, "已复制，粘贴给你的 AI 即可"); if (onAct) onAct(); }, "复制 " + it.app + " 的诊断给 AI"),
      ]),
    ]),
  ]);
}

function notifyContent() {
  const items = attentionItems();
  const frag = document.createDocumentFragment();
  const head = el("div", { class: "notify-head" }, [el("b", { text: items.length ? `需要处理 · ${items.length}` : "需要处理" })]);
  if (items.length) {
    const all = items.map((it) => copyForAI(it.diag, it.app, it.deploymentID)).join("\n\n");
    head.append(button("全部复制给 AI", "btn ghost sm", () => copyToClipboard(all, "已复制，粘贴给你的 AI 即可"), "把全部问题复制给 AI"));
  }
  frag.append(head);
  if (!items.length) {
    frag.append(el("div", { class: "notify-empty" }, [el("span", { class: "dot", "aria-hidden": "true" }), text("一切正常，没有需要处理的事")]));
    return frag;
  }
  const list = el("div", { class: "notify-list" });
  for (const it of items) list.append(alertItemNode(it, closeNotify));
  frag.append(list, el("p", { class: "notify-foot faint", text: "部署失败不影响旧版本继续服务。" }));
  return frag;
}

function openNotify() {
  hidePop();
  const panel = $("notify");
  swapChildren(panel, notifyContent());
  showLayer(panel);
  $("bell").setAttribute("aria-expanded", "true");
  $("bell").classList.add("open");
  // 打开面板就算看过了，之后不再弹卡片
  const seen = loadSeen();
  for (const it of attentionItems()) seen.add(alertKey(it));
  saveSeen(seen);
}

function closeNotify() {
  hideLayer($("notify"));
  $("bell").setAttribute("aria-expanded", "false");
  $("bell").classList.remove("open");
}

function toggleNotify() { if (layerOpen($("notify"))) closeNotify(); else openNotify(); }

function hidePop() {
  clearTimeout(popTimer);
  hideLayer($("notify-pop"));
}

// checkNewAlerts 在每轮数据刷新后调用：有没提醒过的问题就晃铃铛、弹卡片。
function checkNewAlerts() {
  if (!state.loaded || layerOpen($("notify"))) return;
  const seen = loadSeen();
  const fresh = attentionItems().filter((it) => !seen.has(alertKey(it)));
  if (!fresh.length) return;
  for (const it of fresh) seen.add(alertKey(it));
  saveSeen(seen);
  const icon = $("bell").querySelector("svg");
  icon.classList.remove("ring");
  void icon.getBoundingClientRect();
  icon.classList.add("ring");
  const pop = $("notify-pop");
  clear(pop);
  pop.append(alertItemNode(fresh[0], hidePop));
  if (fresh.length > 1) pop.append(el("button", { type: "button", class: "notify-more", onclick: (e) => { e.stopPropagation(); openNotify(); } }, text(`还有 ${fresh.length - 1} 个新问题 ›`)));
  pop.append(button("×", "notify-close", hidePop, "关闭提醒"));
  showLayer(pop);
  clearTimeout(popTimer);
  popTimer = setTimeout(hidePop, 6000);
}

// attentionItems：最多三条，依次为部署失败、未在运行、域名证书失败。
function attentionItems() {
  const items = [];
  for (const a of state.apps) {
    const st = deriveStatus(a);
    const diag = appDiagnosis(a);
    const latest = a.latest || a.live;
    if (st === "failed" && diag) {
      const seq = latest ? `第 ${latest.seq} 版` : "新版本";
      const title = oldVersionServing(a) ? `「${a.name}」${seq}部署失败，旧版本仍在正常服务` : `「${a.name}」${seq}部署失败`;
      items.push({ app: a.name, title, why: stageName(diag.stage) + " · " + diag.message, diag, deploymentID: failedDeploymentID(a) });
    } else if (st === "crashed") {
      const d = diag || { stage: "start", code: "not_running", message: "容器未在运行，但期望状态为运行中。", hint: "查看日志排查启动失败原因，或重新部署。" };
      items.push({ app: a.name, title: `「${a.name}」未在运行`, why: d.message, diag: d, deploymentID: a.current_deployment || "" });
    }
  }
  for (const a of state.apps) {
    const dd = domainDiagnosis(a);
    if (dd) items.push({ app: a.name, title: `「${a.name}」域名 ${dd.domain} 证书未就绪`, why: dd.diagnosis.message, diag: dd.diagnosis, deploymentID: "" });
  }
  return items.slice(0, 3);
}

function stageName(stage) {
  const i = STEP_OF_STAGE[stage];
  return i == null ? "部署" : STEPS[i];
}

function appHash(name, tab) {
  return "#/app/" + encodeURIComponent(name) + (tab && tab !== "overview" ? "/" + tab : "");
}

function appSubLine(a) {
  const latest = a.latest;
  if (!latest) return "还没有版本";
  const st = deriveStatus(a);
  if (st === "deploying") return `第 ${latest.seq} 版 · ${deploymentSource(latest)} · ${deployStatusText(latest.status)}`;
  if (st === "failed") return oldVersionServing(a) ? `第 ${latest.seq} 版失败 · 旧版本仍在服务` : `第 ${latest.seq} 版部署失败`;
  return [`第 ${latest.seq} 版`, deploymentSource(latest), reasonText(latest), timeAgo(latest.created_at)].filter(Boolean).join(" · ");
}

function appCard(a) {
  const st = deriveStatus(a);
  const running = isRunning(a);
  const latest = a.latest;
  const art = el("article", { class: "app-card", dataset: { region: "app:" + a.name } });
  art.append(el("div", { class: "app-top" }, [
    appIcon(a.name),
    el("div", { class: "app-title" }, [
      el("a", { class: "app-name", href: appHash(a.name), text: a.name }),
      el("span", { class: "app-sub", text: appSubLine(a) }),
    ]),
    statusBadge(st),
  ]));
  if (st === "deploying" && latest) {
    art.append(progressNode(STEP_OF_STATUS[latest.status] ?? 0, null, true));
  } else {
    art.append(urlRow(a.url, running && st !== "stopped", a.name));
  }
  let foot;
  if (st === "failed") foot = el("a", { href: appHash(a.name), text: "查看诊断" });
  else if (st === "deploying") foot = el("span", { text: "上传 → 构建 → 启动 → 检查 → 上线" });
  else if (st === "crashed") foot = el("span", { text: "容器未在运行" });
  else foot = el("span", { text: `内存上限 ${a.memory_mb} MB` });
  const on = a.desired !== "stopped";
  art.append(el("div", { class: "app-foot" }, [
    foot,
    switchNode(on, a.name + " 运行开关", () => toggleRun(a.name, on), { disabled: st === "deploying" || !!state.busy[a.name] }),
  ]));
  return art;
}

function urlRow(url, live, name) {
  if (!url) return el("div", { class: "urlrow" }, el("span", { class: "u off", text: "部署成功后显示访问地址" }));
  if (!live) return el("div", { class: "urlrow" }, el("span", { class: "u off", text: url + "（未运行）" }));
  return el("div", { class: "urlrow" }, [
    el("a", { class: "u", href: url, target: "_blank", rel: "noopener", text: url }),
    button("复制", "copy-mini", () => copyToClipboard(url, "网址已复制"), "复制 " + (name || "") + " 的网址"),
  ]);
}

// ambientLine：首页底部的服务器入口，点开是“服务器”页。CPU、内存、磁盘到 80%
// （与服务器页指标条变黄的阈值一致）时黄点并带出那几项，正常时只显示“服务器信息”。
function ambientLine() {
  const h = state.host;
  const ok = h && h.available !== false;
  const unavailable = h && h.available === false; // 还没取到主机数据时按正常显示，不闪灰点
  const hot = [];
  if (ok) {
    if (typeof h.cpu_percent === "number" && h.cpu_percent >= 80) hot.push(`CPU ${Math.round(h.cpu_percent)}%`);
    if (h.memory_total && pct(h.memory_used, h.memory_total) >= 80) hot.push(`内存 ${pct(h.memory_used, h.memory_total)}%`);
    if (h.disk_total && pct(h.disk_used, h.disk_total) >= 80) hot.push(`磁盘 ${pct(h.disk_used, h.disk_total)}%`);
  }
  const dot = unavailable ? "dot stopped" : hot.length ? "dot warning" : "dot";
  const label = ["服务器信息", ...hot].join(" · ");
  return el("a", { class: "server-pill", href: "#/server", "aria-label": "查看" + label, dataset: { region: "ambient" } }, [
    el("span", { class: dot, "aria-hidden": "true" }),
    el("span", { text: label }),
    el("span", { class: "chev", "aria-hidden": "true", text: "›" }),
  ]);
}

// buildStart：没有应用时的首页，也是“新建应用”页。
function buildStart(page, empty) {
  if (!empty) page.append(el("nav", { class: "crumbs", "aria-label": "位置", dataset: { region: "crumbs" } }, [el("a", { href: "#/", text: "我的应用" }), text(" / 新建应用")]));
  page.append(el("div", { class: "start-hero", dataset: { region: "hero" } }, [
    logoSVG(),
    el("h1", { text: empty ? "服务器已就绪，装第一个应用吧" : "新建应用" }),
    el("p", { text: "装好后，应用会出现在“我的应用”里，每个应用一个开关。" }),
  ]));
  const deployCmds = "acornfox deploy\nacornfox deploy --git <公开仓库地址>\nacornfox deploy --image <镜像> --port <端口>";
  page.append(el("div", { class: "cards-3", dataset: { region: "ways" } }, [
    el("section", { class: "way strong" }, [
      el("span", { class: "k", text: "推荐" }),
      el("h2", { text: "让 AI 部署你的项目" }),
      el("p", { text: "把这句话发给 Claude Code、Codex 等 AI 工作台，它会装好 Skill 并完成部署：" }),
      copyBlock(SKILL_PROMPT, "已复制，发给你的 AI 即可", "复制 Skill 安装提示"),
    ]),
    el("section", { class: "way" }, [
      el("span", { class: "k", text: "熟悉终端" }),
      el("h2", { text: "用命令行部署" }),
      el("p", { text: "在你电脑的项目目录里运行（目录需含 Dockerfile；也可以部署公开 Git 仓库或现成镜像）：" }),
      copyBlock(deployCmds, "命令已复制", "复制部署命令", true),
    ]),
    el("section", { class: "way" }, [
      el("span", { class: "k", text: "即将开放" }),
      el("h2", { text: "从应用中心安装" }),
      el("p", { text: "现成的应用选一个、点安装就能用，需要的数据库自动配好。首批是 Hermes 智能体和 New API，v0.5 开放。" }),
      linkButton("看看有哪些", "btn", "#/store"),
    ]),
  ]));
  page.append(el("p", { class: "faint", dataset: { region: "ambient" }, text: "应用通过 http://服务器IP:18810–18899 访问，记得在云厂商安全组放行这些端口；绑定已备案域名再放行 80、443。" }));
}

// ---------------------------------------------------------------------------
// 应用中心（目录服务上线前只做预告）
// ---------------------------------------------------------------------------

function buildStore(page) {
  page.append(el("div", { class: "page-head", dataset: { region: "head" } }, [
    el("div", {}, [el("h1", { text: "应用中心" }), el("p", { class: "sub", text: "挑一个现成应用，点安装就能用。需要的数据库会一起装好。" })]),
  ]));
  page.append(el("div", { class: "notice", role: "note", dataset: { region: "notice" } }, [
    el("b", { text: "即将开放" }),
    el("span", { text: "应用中心在 v0.5 开放，首批是 Hermes 智能体和 New API，到时在这里一键安装。现在可以先让 AI 部署你自己的项目。" }),
  ]));
  const grid = el("div", { class: "store-grid", dataset: { region: "preview" } });
  for (const [name, desc, sub] of STORE_PREVIEW) {
    grid.append(el("article", { class: "store-card" }, [
      el("div", { class: "app-top" }, [
        appIcon(name),
        el("div", { class: "app-title" }, [el("span", { class: "app-name", text: name }), el("span", { class: "app-sub", text: sub })]),
      ]),
      el("p", { class: "desc", text: desc }),
      el("div", { class: "row" }, [el("span", { class: "tag", text: "v0.5 开放" }), el("span", { class: "faint", text: "安装时选择要用的大模型" })]),
    ]));
  }
  page.append(el("section", { class: "stack", dataset: { region: "first" } }, [el("h2", { class: "section-title", text: "首批上架" }), grid]));
  page.append(el("a", { class: "more-link", href: "#/new", dataset: { region: "more" } }, [
    el("span", { class: "stack" }, [el("b", { text: "没有你要的？" }), el("span", { class: "faint", text: "任何带 Dockerfile 的项目都能部署，交给 AI 工作台一句话完成。" })]),
    el("span", { text: "让 AI 部署我的项目 →" }),
  ]));
}

// ---------------------------------------------------------------------------
// 服务器
// ---------------------------------------------------------------------------

function buildServer(page) {
  const h = state.host;
  const ok = h && h.available !== false;
  const sub = [];
  if (ok && h.uptime_seconds) sub.push("已运行 " + uptimeText(h.uptime_seconds));
  if (ok && typeof h.load1 === "number") sub.push("负载 " + h.load1.toFixed(2));
  const vm = ok && h.source === "docker_vm";
  sub.push(vm ? "本机运行 · 下面是 Docker 虚拟机的资源" : "经 SSH 访问 · 控制台不对公网开放");
  page.append(el("div", { class: "page-head", dataset: { region: "head" } }, [
    el("div", {}, [
      el("div", { class: "row" }, [el("h1", { text: state.server || "服务器" }), ok ? badge("running", "运行正常") : badge("stopped", "资源暂不可用")]),
      el("p", { class: "sub", text: sub.join(" · ") }),
    ]),
  ]));
  if (ok) {
    const memoryPct = h.memory_total > 0 ? pct(h.memory_used, h.memory_total) : null;
    const diskPct = h.disk_total > 0 ? pct(h.disk_used, h.disk_total) : null;
    page.append(el("div", { class: "metric-tiles", "aria-label": "主机当前资源", dataset: { region: "tiles" } }, [
      hostTile(vm ? "CPU（虚拟机）" : "CPU", typeof h.cpu_percent === "number" ? numberText(h.cpu_percent, "%") : "采样中", typeof h.cpu_percent === "number" ? h.cpu_percent : null, vm ? `应用容器合计 · 虚拟机 ${h.cpu_cores || "?"} 核` : "最近几秒的平均值"),
      hostTile(vm ? "内存（虚拟机）" : "内存", memoryPct !== null ? memoryPct + "%" : "不可用", memoryPct, vm ? "应用容器合计 " + bytePair(h.memory_used, h.memory_total) : bytePair(h.memory_used, h.memory_total)),
      vm && diskPct === null ? hostTile("磁盘", "—", null, "在 Docker 设置里查看虚拟机磁盘") : hostTile("磁盘", diskPct !== null ? diskPct + "%" : "不可用", diskPct, diskPct !== null && diskPct >= 80 ? "已超过 80%，请清理或扩容" : bytePair(h.disk_used, h.disk_total)),
    ]));
  }
  page.append(hostLimitsCard());
  const rows = state.apps.map((a) => {
    const st = deriveStatus(a);
    return el("tr", {}, [
      el("td", {}, el("a", { href: appHash(a.name), text: a.name })),
      el("td", {}, statusBadge(st)),
      el("td", { class: "mono", text: a.memory_mb + " MB" }),
      el("td", { class: "mono", text: (a.cpu_milli / 1000).toFixed(1) + " 核" }),
      el("td", { class: "mono", text: a.public_port ? String(a.public_port) : "—" }),
    ]);
  });
  const table = el("table", { class: "usage" }, [
    el("thead", {}, el("tr", {}, ["应用", "状态", "内存上限", "CPU 上限", "访问端口"].map((t) => el("th", { text: t })))),
    el("tbody", {}, rows.length ? rows : el("tr", {}, el("td", { colspan: "5", class: "muted", text: "还没有应用。" }))),
  ]);
  page.append(card([cardHead("各应用"), el("div", { class: "scroll-x" }, table)], "", "usage"));
  page.append(el("div", { class: "cards-3", dataset: { region: "more" } }, [
    card([
      el("h2", { class: "section-title", text: "版本与升级" }),
      el("p", { class: "muted", text: "Linux 服务器：升级前自动备份程序和数据库，数据库迁移失败或服务起不来时自动换回原版本；应用容器一直运行，升级期间控制台会暂时连不上。在服务器上执行（Mac、Windows 用 acornfox install 升级）：" }),
      el("span", { class: "faint", text: "中国大陆服务器" }),
      copyBlock(UPGRADE_CN, "升级命令已复制", "复制国内升级命令", true),
      el("span", { class: "faint", text: "其他地区" }),
      copyBlock(UPGRADE_GLOBAL, "升级命令已复制", "复制海外升级命令", true),
    ], "stack"),
    card([
      el("h2", { class: "section-title", text: "访问方式" }),
      el("p", { text: "控制台只经 SSH 隧道访问，不对公网开放。" }),
      el("p", { class: "muted", text: "在你的电脑上运行 acornfox open 打开本页面。应用端口 18810–18899，绑定域名还需放行 80 和 443。" }),
    ], "stack"),
    card([
      el("span", { class: "soon", text: "即将推出" }),
      el("h2", { class: "section-title", text: "不想自己管服务器？" }),
      el("p", { class: "faint", text: "把这台服务器上的应用一键搬到云上，备份、扩容由我们负责。" }),
    ], "stack dark-card"),
  ]));
}

function hostTile(label, value, percent, detail) {
  const fill = el("span");
  if (percent != null && Number.isFinite(percent)) fill.style.width = Math.min(100, Math.max(0, percent)) + "%";
  const bar = el("div", { class: "bar" + (percent != null && percent >= 80 ? " warn" : "") }, fill);
  return el("div", { class: "metric-tile" }, [el("span", { class: "muted", text: label }), el("strong", { text: value }), bar, detail ? el("span", { class: "faint", text: detail }) : null]);
}

function bytePair(used, total) {
  if (!Number.isFinite(used) || !Number.isFinite(total) || total <= 0) return "";
  return (used / (1024 ** 3)).toFixed(1) + " / " + (total / (1024 ** 3)).toFixed(1) + " GiB";
}

function uptimeText(s) {
  const d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600);
  if (d) return `${d} 天 ${h} 小时`;
  const m = Math.floor((s % 3600) / 60);
  return h ? `${h} 小时 ${m} 分钟` : `${m} 分钟`;
}

// ---------------------------------------------------------------------------
// 设置
// ---------------------------------------------------------------------------

function buildSettings(page) {
  page.append(el("div", { class: "page-head", dataset: { region: "head" } }, el("h1", { text: "设置" })));
  const mode = themeMode();
  const seg = el("div", { class: "seg", role: "radiogroup", "aria-label": "外观" });
  for (const [id, label] of [["system", "跟随系统"], ["light", "浅色"], ["dark", "深色"]]) {
    seg.append(el("button", { type: "button", role: "radio", "aria-checked": mode === id ? "true" : "false", onclick: () => { setTheme(id === "system" ? null : id); render(); }, text: label }));
  }
  page.append(card(el("div", { class: "setting" }, [
    el("div", { class: "text" }, [el("h2", { text: "外观" }), el("span", { class: "faint", text: "只在这台电脑的浏览器里生效。" })]), seg,
  ]), "", "theme"));
  page.append(card([
    el("h2", { class: "section-title", text: "连接 AI 工作台" }),
    el("p", { class: "muted", text: "把这句话发给 Claude Code、Codex 等 AI 工作台，装好 Skill 后，部署、排错、加数据库都可以用一句话完成。" }),
    copyBlock(SKILL_PROMPT, "已复制，发给你的 AI 即可", "复制 Skill 安装提示"),
  ], "stack", "ai"));
  page.append(card([
    el("h2", { class: "section-title", text: "修改应用配置" }),
    el("p", { class: "muted", text: "环境变量、数据目录、资源上限在控制台里只读。请在 AI 工作台里说明要改什么，或在你的电脑上使用 acornfox env set、acornfox volume add、acornfox app set，改完点“重新部署”生效。密钥值在任何地方都不会显示。" }),
  ], "stack", "config"));
  page.append(card(el("div", { class: "setting" }, [
    el("div", { class: "text" }, [el("h2", { text: "登录会话" }), el("span", { class: "faint", text: "关闭浏览器后自动失效，再次使用请运行 acornfox open。" })]),
    button("退出登录", "btn", logout, "退出登录"),
  ]), "", "session"));
  page.append(el("div", { class: "footer-links", dataset: { region: "links" } }, [
    el("span", { text: "AcornFox · AGPL-3.0 开源" }),
    el("a", { href: "https://acornfox.com", target: "_blank", rel: "noopener", text: "官网" }),
    el("a", { href: "https://github.com/OrbitMaker/AcornFox", target: "_blank", rel: "noopener", text: "GitHub" }),
    el("a", { href: "https://gitee.com/VIP13390/AcornFox", target: "_blank", rel: "noopener", text: "Gitee" }),
    el("a", { href: "https://github.com/OrbitMaker/AcornFox/issues", target: "_blank", rel: "noopener", text: "问题反馈" }),
  ]));
}

// ---------------------------------------------------------------------------
// 应用详情
// ---------------------------------------------------------------------------

function buildApp(page) {
  const name = state.openApp;
  const listApp = state.apps.find((x) => x.name === name);
  const a = state.detail || listApp;
  page.append(el("nav", { class: "crumbs", "aria-label": "位置", dataset: { region: "crumbs" } }, [el("a", { href: "#/", text: "我的应用" }), text(" / " + name)]));
  if (!a) {
    page.append(card([el("h2", { class: "section-title", text: "找不到应用「" + name + "」" }), el("p", { class: "muted", text: "它可能已被删除。" }), linkButton("回到我的应用", "btn", "#/")], "stack", "missing"));
    return;
  }
  const st = deriveStatus(a);
  const running = isRunning(a);
  const on = a.desired !== "stopped";
  const sub = [];
  if (a.live) sub.push(`第 ${a.live.seq} 版`, deploymentSource(a.live), reasonText(a.live), timeAgo(a.live.created_at));
  else if (a.latest) sub.push(`第 ${a.latest.seq} 版`, deployStatusText(a.latest.status));
  else sub.push("还没有版本");
  if (st === "failed" && oldVersionServing(a) && a.latest) sub.push(`第 ${a.latest.seq} 版未上线`);
  const side = el("div", { class: "row" });
  if (a.url && running) side.append(el("a", { class: "btn", href: a.url, target: "_blank", rel: "noopener", text: "打开网站" }));
  side.append(el("span", { class: "run-pill" + (on && running ? " on" : "") }, [
    el("span", { text: !on ? "已停止" : st === "deploying" ? "部署中" : running ? "运行中" : "未在运行" }),
    switchNode(on, a.name + " 运行开关", () => toggleRun(a.name, on), { disabled: st === "deploying" || !!state.busy[a.name] }),
  ]));
  page.append(el("div", { class: "detail-head", dataset: { region: "head" } }, [
    appIcon(a.name, "lg"),
    el("div", { class: "title" }, [
      el("div", { class: "row" }, [el("h1", { text: a.name }), statusBadge(st, st === "failed" && oldVersionServing(a) ? "新版本部署失败" : null)]),
      el("span", { class: "muted", text: sub.filter(Boolean).join(" · ") }),
    ]),
    side,
  ]));
  const diag = appDiagnosis(a);
  if (diag && state.detailTab !== "logs") page.append(diagSection(diag, a));
  else if (st === "deploying" && a.latest) {
    page.append(card([
      cardHead(`第 ${a.latest.seq} 版正在部署`, el("span", { class: "faint", text: deployStatusText(a.latest.status) })),
      progressNode(STEP_OF_STATUS[a.latest.status] ?? 0, null, false),
    ], "", "progress"));
  }
  const tabs = el("div", { class: "tabs", role: "tablist", "aria-label": "应用详情", dataset: { region: "tabs" } });
  for (const [k, label] of APP_TABS) {
    tabs.append(el("button", { type: "button", role: "tab", "aria-selected": state.detailTab === k ? "true" : "false", onclick: () => go(appHash(a.name, k)), text: label }));
  }
  page.append(tabs);
  const body = el("div", { class: "stack", role: "tabpanel", dataset: { region: "tab:" + state.detailTab } });
  if (!state.detail) body.append(el("p", { class: "muted", text: "正在加载…" }));
  else if (state.detailTab === "logs") body.append(card(logsTab(a), "stack"));
  else if (state.detailTab === "versions") body.append(versionsTab(a));
  else if (state.detailTab === "config") body.append(configTab(a));
  else if (state.detailTab === "db") body.append(addonsTab(a));
  else if (state.detailTab === "domains") body.append(card(domainsTab(a), "stack"));
  else body.append(overviewTab(a));
  page.append(body);
}

// missingAddon：诊断里提到缺少数据库连接、而应用还没有对应数据库时，给出一键添加。
function missingAddon(diag, a) {
  const textAll = [diag.message, diag.log_excerpt, diag.hint].join("\n");
  const has = (k) => (a.addons || []).some((x) => x.kind === k);
  if (/REDIS_URL/.test(textAll) && !has("redis")) return "redis";
  if (/DATABASE_URL/.test(textAll) && !has("postgres") && !has("mysql")) return /mysql/i.test(textAll) ? "mysql" : "postgres";
  return null;
}

function diagSection(diag, a) {
  const latest = a.latest || a.live;
  const seq = latest ? `第 ${latest.seq} 版` : "这次部署";
  const serving = oldVersionServing(a);
  const sec = el("section", { class: "diag", role: "alert", "aria-label": "部署诊断", dataset: { region: "diag" } });
  sec.append(el("div", { class: "stack" }, [
    el("span", { class: "kicker", text: "诊断 · " + seq }),
    el("h2", { text: `${seq}没有上线：${diag.message || "部署失败"}` }),
    el("span", { class: "safe", text: serving ? "用户访问不受影响，旧版本一直在服务。" : "应用还没有上线的版本。" }),
  ]));
  const step = STEP_OF_STAGE[diag.stage];
  if (step != null) sec.append(progressNode(null, step, false));
  if (diag.log_excerpt) {
    const lines = diag.log_excerpt.split("\n");
    sec.append(el("div", { class: "stack" }, [
      el("b", { text: "关键日志" }),
      el("pre", { class: "excerpt", tabindex: "0", text: lines.slice(-14).join("\n") }),
    ]));
  }
  if (diag.hint) sec.append(el("div", { class: "hint" }, [el("b", { text: "建议" }), el("span", { text: diag.hint })]));
  const kind = missingAddon(diag, a);
  const forAI = copyForAI(diag, a.name, failedDeploymentID(a));
  const row = el("div", { class: "row" });
  if (kind) row.append(button(`添加 ${ADDON_NAMES[kind]} 并重新部署`, "btn primary knob", () => addAddon(a.name, kind), `添加 ${ADDON_NAMES[kind]} 并重新部署`));
  row.append(button("复制诊断给 AI", kind ? "btn" : "btn primary", () => copyToClipboard(forAI, "已复制，粘贴给你的 AI 即可"), "复制诊断给 AI"));
  row.append(linkButton("查看完整日志", "btn ghost", appHash(a.name, "logs")));
  sec.append(row);
  sec.append(el("span", { class: "faint", text: "复制给 AI 的内容包含阶段、错误码、日志片段和建议，可直接粘贴到 Claude Code 或 Codex。" }));
  return sec;
}

function overviewTab(a) {
  const running = isRunning(a);
  const left = el("div", { class: "stack" });
  const right = el("div", { class: "stack" });

  const urls = el("div", { class: "stack" });
  for (const d of a.domains || []) {
    if (d.status === "ready") urls.append(el("div", { class: "row" }, [urlRow("https://" + d.name, running, a.name), badge("running", "HTTPS 已生效")]));
  }
  urls.append(urlRow(a.url, running && a.desired !== "stopped", a.name));
  if (a.lan_url) urls.append(el("div", { class: "row" }, [urlRow(a.lan_url, running && a.desired !== "stopped", a.name), el("span", { class: "faint", text: "局域网" })]));
  const expoText = { local: "只本机能打开", lan: "本机和局域网能打开", public: "所有网卡和域名", afx_cloud: "经 AFX 云" }[a.exposure];
  if (expoText) urls.append(el("p", { class: "faint", text: "暴露方式：" + expoText + "（acornfox app set --app " + a.name + " --expose local|lan|public）" }));
  left.append(card([cardHead("访问地址"), urls], "", "urls"));

  left.append(card([
    cardHead("资源", el("span", { class: "row" }, [
      el("span", { class: "faint", text: `上限 ${a.memory_mb} MB 内存 · ${(a.cpu_milli / 1000).toFixed(1)} 核` }),
      // 跳到“配置”标签并把资源上限卡片滚到视口中间（onRoute 里处理）
      linkButton("调整上限 →", "btn ghost sm", appHash(a.name, "config"), { onclick: () => { state.focusLimits = true; } }),
    ])),
    running ? appMetricsPanel(a) : el("p", { class: "muted", text: "应用未运行，没有资源数据。" }),
  ], "", "resources"));

  const recent = (state.deployments || []).slice(0, 3);
  const ul = el("ul", { class: "list" });
  if (!state.deployments) ul.append(el("li", { class: "muted", text: "正在加载…" }));
  else if (!recent.length) ul.append(el("li", { class: "muted", text: "还没有部署记录。" }));
  for (const d of recent) ul.append(versionRow(d, a, true));
  left.append(card([cardHead("版本记录", linkButton("全部 →", "btn ghost sm", appHash(a.name, "versions"))), ul], "", "recent"));

  const dbl = el("ul", { class: "list" });
  const addons = a.addons || [];
  if (!addons.length) dbl.append(el("li", { class: "muted", text: "未添加数据库。需要时在“数据库”标签里打开开关。" }));
  for (const x of addons) {
    dbl.append(el("li", {}, [
      el("span", { class: "v", text: ADDON_NAMES[x.kind] || x.kind }),
      el("span", { class: "meta", text: "本机 · 已注入 " + (x.env_var || "") }),
      badge(addonBadgeCls(x.observed_state), addonStateText(x.observed_state)),
    ]));
  }
  right.append(card([cardHead("数据库", linkButton("管理 →", "btn ghost sm", appHash(a.name, "db"))), dbl], "", "db"));

  const actions = el("div", { class: "row" });
  if (a.desired === "stopped") actions.append(button("启动", "btn primary", () => actOnApp("start"), "启动应用"));
  else if (running) actions.append(button("重启", "btn", () => actOnApp("restart"), "重启应用"));
  if (a.live) actions.append(button("重新部署", "btn", () => actOnApp("redeploy"), "用当前版本重新部署"));
  if (a.live && a.live.seq > 1) actions.append(button("回退到上一版", "btn", confirmRollback, "回退到上一版"));
  actions.append(button("复制状态给 AI", "btn", () => copyToClipboard(statusForAI(a), "已复制，粘贴给你的 AI 即可"), "复制状态给 AI"));
  right.append(card([cardHead("常用操作"), actions], "", "actions"));

  const env = a.env || [];
  const secrets = env.filter((e) => e.secret).length;
  const vols = a.volumes || [];
  const kv = el("dl", { class: "kv" }, [
    el("dt", { text: "环境变量" }), el("dd", { text: env.length ? `${env.length} 个（含密钥 ${secrets} 个）` : "无" }),
    el("dt", { text: "数据目录" }), el("dd", { class: "mono", text: vols.length ? vols.map((v) => v.path).join("、") : "无" }),
    el("dt", { text: "容器端口" }), el("dd", { class: "mono", text: String(a.port || 0) }),
  ]);
  right.append(card([cardHead("配置", linkButton("查看 →", "btn ghost sm", appHash(a.name, "config"))), kv], "", "config"));

  return el("div", { class: "grid-2" }, [left, right]);
}

function addonBadgeCls(s) {
  return ({ running: "running", starting: "warning", missing: "warning", unhealthy: "error" })[s] || "stopped";
}

function versionRow(d, a, compact) {
  const li = el("li");
  li.append(el("span", { class: "v", text: "第 " + d.seq + " 版" }));
  if (d.status === "live") li.append(badge("running", "正在服务"));
  else if (d.status === "failed") li.append(badge("error", "失败"));
  else if (PENDING_STATUSES.has(d.status)) li.append(badge("deploying", deployStatusText(d.status)));
  const meta = [deploymentSource(d), reasonText(d), timeAgo(d.created_at)];
  if (d.status === "live" || d.status === "retired") meta.push(tookText(d));
  if (d.status === "failed" && d.diagnosis) meta.push("停在" + stageName(d.diagnosis.stage) + "：" + d.diagnosis.message);
  else if (!compact && d.status !== "live") meta.push(deployStatusText(d.status));
  li.append(el("span", { class: "meta", text: meta.filter(Boolean).join(" · ") }));
  return li;
}

function versionsTab(a) {
  const list = state.deployments;
  const side = a.live && a.live.seq > 1 ? button("回退到上一版", "btn sm", confirmRollback, "回退到上一版") : null;
  const ul = el("ul", { class: "list" });
  if (!list) ul.append(el("li", { class: "muted", text: "正在加载…" }));
  else if (!list.length) ul.append(el("li", { class: "muted", text: "还没有部署记录。" }));
  else for (const d of list) ul.append(versionRow(d, a, false));
  return card([
    cardHead("版本记录", side),
    ul,
    el("p", { class: "faint", text: "保留最近的版本，可回退到上一版。回退不重新构建，直接用那一版的镜像。" }),
  ], "stack");
}

function configTab(a) {
  const frag = document.createDocumentFragment();
  const env = a.env || [];
  const envList = el("ul", { class: "list envlist" });
  if (!env.length) envList.append(el("li", { class: "muted", text: "没有环境变量。" }));
  for (const e of env) {
    envList.append(el("li", {}, [
      el("span", { class: "k", text: e.key }),
      el("span", { class: "grow" }),
      e.secret ? badge("stopped", "密钥 · 已设置") : el("span", { class: "mono muted", text: e.value || "" }),
    ]));
  }
  frag.append(card([cardHead("环境变量"), envList], "", "env"));
  const vols = a.volumes || [];
  const volList = el("ul", { class: "list envlist" });
  if (!vols.length) volList.append(el("li", { class: "muted", text: "没有数据目录。重新部署时，容器里未保存到数据目录的文件会丢失。" }));
  for (const v of vols) volList.append(el("li", {}, [el("span", { class: "k", text: v.path }), el("span", { class: "grow" }), el("span", { class: "faint", text: v.auto ? "自动识别" : "手动添加" })]));
  frag.append(card([cardHead("数据目录"), volList], "", "volumes"));
  frag.append(card([cardHead("运行参数"), el("dl", { class: "kv" }, [
    el("dt", { text: "容器端口" }), el("dd", { class: "mono", text: String(a.port || 0) }),
    el("dt", { text: "访问端口" }), el("dd", { class: "mono", text: a.public_port ? String(a.public_port) : "—" }),
    el("dt", { text: "健康检查" }), el("dd", { class: "mono", text: a.health_path || "/" }),
  ])], "", "params"));
  frag.append(appLimitsCard(a));
  frag.append(card([
    cardHead("怎么修改"),
    el("p", { class: "muted", text: "内存和 CPU 上限在上面直接拖动修改。其余配置在 AI 工作台里说明要改什么，或在你的电脑上运行下面的命令，改完点“重新部署”生效。密钥值不会显示或返回。" }),
    copyBlock(`acornfox env set KEY=VALUE --app ${a.name}\nacornfox env set KEY=VALUE --secret --app ${a.name}\nacornfox volume add /容器内目录 --app ${a.name}`, "命令已复制", "复制配置命令", true),
  ], "stack", "how"));
  return frag;
}

// ---------------------------------------------------------------------------
// 资源上限：应用的内存、CPU 上限拖动修改，立即生效不重启；服务器页设置整机
// 最多分配给应用的内存和 CPU。内存严格不超分，CPU 只限单个应用。规则以服务端为准，
// 这里只把滑块范围限制在服务端会接受的区间。
// ---------------------------------------------------------------------------

function needsResources() {
  const r = state.route;
  return r.page === "server" || (r.page === "app" && state.detailTab === "config");
}

let resourcesGen = 0;

async function loadResources(rerender) {
  const gen = ++resourcesGen;
  try {
    const body = await api.get("/v1/resources");
    if (gen !== resourcesGen) return; // 同 detailGen：晚到的旧结果不覆盖新的
    state.resources = body;
    if (rerender) render();
  } catch (e) { if (rerender) handleError(e); }
}

function mbText(mb) { return mb >= 1024 ? (mb / 1024).toFixed(1) + " GB" : Math.round(mb) + " MB"; }
function coreText(milli) { return (milli / 1000).toFixed(1) + " 核"; }
function roundUp(v, step) { return Math.ceil(v / step) * step; }

// limitSlider 是一行“名称 + 当前值 + 滑块 + 两端说明”。onInput 只更新草稿和这一行的文字，
// 不触发整页重绘，拖动才不会被打断。log 为真时用对数刻度：内存范围可能从 256 MB 到几十 GB，
// 均匀刻度下常用的几百 MB 挤在最左边拖不准。
function limitSlider(o) {
  const max = Math.max(o.min, o.max);
  const useLog = o.log && max > o.min * 4;
  const toValue = (p) => useLog
    ? Math.min(max, Math.max(o.min, Math.round(o.min * Math.pow(max / o.min, p / 1000) / o.step) * o.step))
    : p;
  const toPos = (v) => useLog ? Math.round(1000 * Math.log(v / o.min) / Math.log(max / o.min)) : v;
  const value = el("span", { class: "limit-value mono", text: o.format(o.value) });
  const input = el("input", useLog
    ? { type: "range", min: "0", max: "1000", step: "1", value: String(toPos(o.value)), "aria-label": o.label, "aria-valuetext": o.format(o.value) }
    : { type: "range", min: String(o.min), max: String(max), step: String(o.step), value: String(o.value), "aria-label": o.label, "aria-valuetext": o.format(o.value) });
  input.addEventListener("input", () => {
    const v = toValue(Number(input.value));
    value.textContent = o.format(v);
    input.setAttribute("aria-valuetext", o.format(v));
    o.onInput(v, input);
  });
  return el("div", { class: "limit-row" }, [
    el("div", { class: "limit-top" }, [el("span", { text: o.label }), value]),
    input,
    el("div", { class: "limit-ends faint" }, [el("span", { text: o.left }), el("span", { text: o.right })]),
  ]);
}

// startDraft 在第一次拖动时建草稿、标记卡片正在编辑，并显示保存/取消。
function startDraft(key, base, cardNode) {
  if (!state.limitDraft || state.limitDraft.key !== key) state.limitDraft = Object.assign({ key }, base);
  cardNode.dataset.editing = "1";
  const actions = cardNode.querySelector(".limit-actions");
  if (actions) actions.hidden = false;
}

function endDraft(cardNode) {
  state.limitDraft = null;
  if (cardNode) delete cardNode.dataset.editing;
}

function appLimitsCard(a) {
  const key = "app:" + a.name;
  const res = state.resources;
  const draft = state.limitDraft && state.limitDraft.key === key ? state.limitDraft : null;
  const memNow = a.memory_mb > 0 ? a.memory_mb : 256;
  const cpuNow = a.cpu_milli > 0 ? a.cpu_milli : 1000;
  const minMem = (res && res.min_app_memory_mb) || 256;
  const minCPU = (res && res.min_app_cpu_milli) || 500;
  const node = card([], "stack", "limits:" + a.name);
  node.dataset.atomic = "1";
  node.append(cardHead("资源上限", el("span", { class: "faint", text: "保存后立即生效，应用不重启" })));
  if (!res) { node.append(el("p", { class: "muted", text: "正在读取整机分配情况…" })); return node; }

  // 内存最多 = 整机上限 − 其他容器已分配；没有上限时沿用旧的 16 GB。
  const memCap = res.memory.cap_mb > 0 ? res.memory.cap_mb : 16384;
  const memMax = Math.max(memNow, Math.floor((res.memory.cap_mb > 0 ? memCap - res.memory.allocated_mb + memNow : memCap) / 64) * 64);
  const usage = state.appMetrics && Number.isFinite(state.appMetrics.memory_usage_mb) && state.appMetrics.memory_usage_mb > 0 ? state.appMetrics.memory_usage_mb : null;
  // 运行中的应用不能调到当前用量以下（服务端留 32 MB 余量）
  const memMin = usage ? Math.min(memNow, Math.max(minMem, roundUp(usage + 32, 64))) : minMem;
  const cpuMax = Math.max(cpuNow, res.cpu.cap_milli > 0 ? res.cpu.cap_milli : 16000);
  const mem = draft ? draft.memory : memNow;
  const cpu = draft ? draft.cpu : cpuNow;
  const hint = el("span", { class: "faint", text: "" });
  const onInput = (field) => (v) => {
    startDraft(key, { memory: memNow, cpu: cpuNow }, node);
    state.limitDraft[field] = v;
    hint.textContent = state.limitDraft.memory > memNow ? `整机内存还剩 ${mbText(memMax - state.limitDraft.memory)} 可分配` : "";
  };
  node.append(limitSlider({
    label: "内存", value: mem, min: memMin, max: memMax, step: 64, log: true, format: mbText, onInput: onInput("memory"),
    left: usage ? `最少 ${mbText(memMin)} · 现在用了 ${mbText(usage)}` : `最少 ${mbText(memMin)}`,
    right: `最多 ${mbText(memMax)}`,
  }));
  node.append(limitSlider({
    label: "CPU", value: cpu, min: minCPU, max: cpuMax, step: 100, format: coreText, onInput: onInput("cpu"),
    left: `最少 ${coreText(minCPU)}`, right: `最多 ${coreText(cpuMax)}`,
  }));
  const actions = el("div", { class: "limit-actions", hidden: draft ? false : true }, [
    hint,
    el("span", { class: "grow" }),
    button("取消", "btn", () => { endDraft(node); render(); }, "取消修改资源上限"),
    button("保存", "btn primary", () => saveAppLimits(a.name, node), "保存资源上限"),
  ]);
  if (draft) node.dataset.editing = "1";
  node.append(actions);
  return node;
}

async function saveAppLimits(name, node) {
  const d = state.limitDraft;
  if (!d || state.busy["limits:" + name]) return;
  state.busy["limits:" + name] = true;
  try {
    const body = await api.send("PATCH", appPath(name), { memory_mb: d.memory, cpu_milli: d.cpu });
    // 先用返回的新值更新页面再清草稿，并让保存前发出的详情请求作废，免得闪回旧值
    if (body.app && state.openApp === name) { detailGen++; state.detail = Object.assign({}, state.detail, body.app); }
    endDraft(node);
    if (body.applied_now) toast("已保存，立即生效，应用没有重启");
    else if (body.apply_error) toast(body.apply_error);
    else toast("已保存，下次部署时生效");
    await Promise.all([loadDetail(name), loadResources(false)]);
  } catch (e) {
    handleError(e);
  } finally {
    delete state.busy["limits:" + name];
    render();
  }
}

const ALLOC_COLORS = ["var(--brand)", "var(--alloc-2)", "var(--alloc-3)", "var(--alloc-4)"];

function hostLimitsCard() {
  const res = state.resources;
  const node = card([], "stack", "host-limits");
  node.dataset.atomic = "1";
  node.append(cardHead("分配给应用的资源", el("span", { class: "faint", text: "内存加起来不能超过上限；CPU 只限制单个应用" })));
  if (!res) { node.append(el("p", { class: "muted", text: "正在读取…" })); return node; }
  const m = res.memory, c = res.cpu;
  if (!(m.host_mb > 0)) {
    node.append(el("p", { class: "muted", text: `已分配内存 ${mbText(m.allocated_mb)}。读不到整机内存，暂时不能设置上限。` }));
    return node;
  }
  const draft = state.limitDraft && state.limitDraft.key === "host" ? state.limitDraft : null;
  const capMem = draft ? draft.memory : m.cap_mb;
  const capCPU = draft ? draft.cpu : c.cap_milli;

  // 条形图：每个应用和数据库一段，后面是还可分配、留给系统和其他程序。
  const bar = el("div", { class: "alloc-bar", role: "img" });
  const legend = el("div", { class: "alloc-legend" });
  const width = (mb) => Math.max(0, mb / m.host_mb * 100).toFixed(2) + "%";
  const appColor = {};
  res.items.forEach((it) => {
    if (!(it.app in appColor)) appColor[it.app] = ALLOC_COLORS[Object.keys(appColor).length % ALLOC_COLORS.length];
    const label = it.kind === "app" ? it.app : `${it.app} · ${ADDON_NAMES[it.kind] || it.kind}`;
    const seg = el("span", { class: "seg" + (it.kind === "app" ? "" : " addon"), title: `${label}：${mbText(it.memory_mb)}` });
    seg.style.width = width(it.memory_mb);
    seg.style.background = appColor[it.app];
    bar.append(seg);
  });
  const free = el("span", { class: "seg free", title: "还可分配" });
  const freeMB = Math.max(0, capMem - m.allocated_mb);
  free.style.width = width(freeMB);
  bar.append(free);
  const reserved = el("span", { class: "seg reserved", title: "留给系统和其他程序" });
  reserved.style.width = width(Math.max(0, m.host_mb - Math.max(capMem, m.allocated_mb)));
  bar.append(reserved);
  bar.setAttribute("aria-label", `内存已分配 ${mbText(m.allocated_mb)}，上限 ${mbText(capMem)}，整机 ${mbText(m.host_mb)}`);
  for (const app of Object.keys(appColor)) {
    const mb = res.items.filter((it) => it.app === app).reduce((sum, it) => sum + it.memory_mb, 0);
    const sw = el("i", { "aria-hidden": "true" });
    sw.style.background = appColor[app];
    legend.append(el("span", {}, [sw, text(`${app} ${mbText(mb)}`)]));
  }
  legend.append(el("span", {}, [el("i", { class: "free", "aria-hidden": "true" }), text(`还可分配 ${mbText(freeMB)}`)]));
  legend.append(el("span", {}, [el("i", { class: "reserved", "aria-hidden": "true" }), text("留给系统和其他程序")]));

  const summary = el("span", { class: "faint", text: `已分配 ${mbText(m.allocated_mb)} / 上限 ${mbText(capMem)}${m.auto && !draft ? "（自动）" : ""} · 整机 ${mbText(m.host_mb)}` });
  node.append(el("div", { class: "limit-top" }, [el("b", { text: "内存" }), summary]));
  node.append(bar, legend);

  const memMin = Math.max(res.min_app_memory_mb || 256, roundUp(m.allocated_mb, 256));
  const memMax = Math.floor(m.host_mb / 256) * 256;
  const onInput = (field) => (v) => {
    startDraft("host", { memory: m.cap_mb, cpu: c.cap_milli }, node);
    state.limitDraft[field] = v;
    if (field === "memory") {
      const f = Math.max(0, v - m.allocated_mb);
      free.style.width = width(f);
      reserved.style.width = width(Math.max(0, m.host_mb - Math.max(v, m.allocated_mb)));
      summary.textContent = `已分配 ${mbText(m.allocated_mb)} / 上限 ${mbText(v)} · 整机 ${mbText(m.host_mb)}`;
    }
  };
  node.append(limitSlider({
    label: "最多分配给应用", value: capMem, min: memMin, max: memMax, step: 256, format: mbText, onInput: onInput("memory"),
    left: `不能低于已分配的 ${mbText(m.allocated_mb)}`, right: `整机 ${mbText(m.host_mb)}`,
  }));
  if (c.host_milli > 0) {
    node.append(limitSlider({
      label: "单个应用 CPU 最多", value: capCPU, min: Math.max(res.min_app_cpu_milli || 500, roundUp(c.max_app_milli, 500)), max: c.host_milli, step: 500,
      format: coreText, onInput: onInput("cpu"),
      left: `不能低于已设的 ${coreText(c.max_app_milli)}`, right: `整机 ${coreText(c.host_milli)}${c.auto && !draft ? " · 自动" : ""}`,
    }));
  }
  const autoBtn = !m.auto || !c.auto ? button("恢复自动", "btn ghost", () => saveHostLimits(node, { memory_mb: 0, cpu_milli: 0 }), "恢复自动上限") : null;
  const actions = el("div", { class: "limit-actions", hidden: draft ? false : true }, [
    el("span", { class: "faint", text: "自动：内存为整机减去留给系统的 1 GB 或 15%，CPU 为全部核心" }),
    el("span", { class: "grow" }),
    button("取消", "btn", () => { endDraft(node); render(); }, "取消修改整机上限"),
    button("保存", "btn primary", () => saveHostLimits(node, { memory_mb: state.limitDraft.memory, cpu_milli: state.limitDraft.cpu }), "保存整机上限"),
  ]);
  if (draft) node.dataset.editing = "1";
  node.append(actions);
  if (autoBtn && !draft) node.append(el("div", { class: "row" }, [el("span", { class: "faint", text: "已手动设置上限。" }), autoBtn]));
  return node;
}

async function saveHostLimits(node, body) {
  if (state.busy.hostLimits) return;
  state.busy.hostLimits = true;
  try {
    const updated = await api.send("PUT", "/v1/resources", body);
    resourcesGen++; // 让保存前发出的读取作废
    state.resources = updated;
    endDraft(node);
    toast(body.memory_mb === 0 && body.cpu_milli === 0 ? "已恢复自动上限" : "整机上限已保存");
  } catch (e) {
    handleError(e);
  } finally {
    delete state.busy.hostLimits;
    render();
  }
}

// addonsTab 是“数据库”标签。只渲染公开字段：连接地址、密码等凭据永远不进页面。
function addonsTab(a) {
  const frag = document.createDocumentFragment();
  const addons = Array.isArray(a.addons) ? a.addons : [];
  frag.append(el("p", { class: "muted", text: "需要时自动装上：应用中心的应用按模板带上，自己的项目在诊断发现缺少数据库时一键添加。每个应用独立一套，只在应用内部网络可见，密码自动生成、不会显示。" }));
  if (addons.length === 0) {
    frag.append(el("p", { text: "此应用还没有附加服务（数据库）。需要时打开下面的开关，或在终端运行：" }));
    frag.append(el("p", { class: "mono", text: "acornfox add postgres --app " + a.name }));
  }
  const kinds = ADDON_KINDS.slice();
  for (const x of addons) if (!kinds.includes(x.kind)) kinds.push(x.kind);
  for (const kind of kinds) {
    const addon = addons.find((x) => x.kind === kind);
    const label = ADDON_NAMES[kind] || kind || "附加服务";
    const conflict = !addon && (kind === "postgres" || kind === "mysql") ? addons.find((x) => (x.kind === "postgres" || x.kind === "mysql") && x.kind !== kind) : null;
    const box = el("article", { class: "addon-card", "aria-label": label + " 状态" });
    let desc = ADDON_DESC[kind] || "";
    if (!addon) desc = conflict ? `已有 ${ADDON_NAMES[conflict.kind]} 提供 DATABASE_URL，同一应用只能有其一。` : "打开即添加，已上线的应用会自动重新部署。";
    box.append(el("div", { class: "head" }, [
      el("h4", { text: label }),
      addon ? badge(addonBadgeCls(addon.observed_state), addonStateText(addon.observed_state)) : null,
      el("span", { class: "desc", text: addon ? ADDON_DESC[kind] || "" : desc }),
      switchNode(!!addon, label, () => (addon ? confirmRemoveAddon(a.name, kind) : addAddon(a.name, kind)), { disabled: !!conflict || !!state.busy[a.name + ":" + kind] }),
    ]));
    if (addon) {
      const dl = el("dl", { class: "kv" });
      for (const [k, value] of [
        ["来源", "本机容器"],
        ["镜像", addon.image || "未知"],
        ["网络内地址", addon.host ? addon.host + ":" + addon.port : "暂不可用"],
        ["注入变量", addon.env_var || "未设置"],
        ["数据卷", addon.volume_name || "未知"],
      ]) {
        dl.append(el("dt", { text: k }));
        dl.append(el("dd", { text: value }));
      }
      box.append(dl);
    }
    frag.append(box);
  }
  frag.append(el("p", { class: "faint", text: "端口仅在应用专用网络内可见。连接信息以密钥注入应用，控制台不显示密码。关闭数据库时默认保留数据，确认框里可以选择一并删除。" }));
  frag.append(el("p", { class: "faint", text: "使用外部数据库（云数据库或另一台服务器）：acornfox env set DATABASE_URL=<连接地址> --secret --app " + a.name }));
  return frag;
}

function domainsTab(a) {
  const frag = document.createDocumentFragment();
  const domains = a.domains || [];
  frag.append(cardHead("域名"));
  if (!domains.length) {
    frag.append(el("p", { class: "muted", text: "还没有绑定域名，目前通过 IP:端口 访问。" }));
  } else {
    const ul = el("ul", { class: "list" });
    for (const d of domains) {
      const cls = d.status === "ready" ? "running" : d.status === "failed" ? "error" : "warning";
      const label = ({ ready: "HTTPS 已生效", failed: "证书失败", pending: "等待证书" })[d.status] || d.status;
      ul.append(el("li", {}, [el("span", { class: "v mono", text: d.name }), badge(cls, label), el("span", { class: "meta", text: d.diagnosis && d.status === "failed" ? d.diagnosis.message : "" })]));
    }
    frag.append(ul);
    const dd = domainDiagnosis(a);
    if (dd && dd.diagnosis.hint) frag.append(el("div", { class: "hint" }, [el("b", { text: "建议" }), el("span", { text: dd.diagnosis.hint })]));
  }
  frag.append(el("p", { class: "muted", text: "绑定域名：先把域名解析到这台服务器的公网 IP，再运行下面的命令，证书会自动申请。" }));
  frag.append(copyBlock("acornfox domain add 你的域名 --app " + a.name, "命令已复制", "复制绑定域名命令", true));
  frag.append(el("p", { class: "faint", text: "中国大陆服务器的域名须已完成 ICP 备案，否则无法通过 80/443 访问；未备案时仍可用 IP:端口 访问。需要在安全组放行 80 和 443。" }));
  return frag;
}

function logsTab(a) {
  const frag = document.createDocumentFragment();
  const tail = el("select", { id: "logs-tail", "aria-label": "日志行数", onchange: (event) => {
    state.logTail = Number(event.target.value);
    refreshLogs(a.name, true);
  } });
  for (const value of [100, 500, 1000]) tail.append(el("option", { value, text: "最近 " + value + " 行" }));
  tail.value = String(state.logTail);
  const title = a.live ? `应用日志 · 第 ${a.live.seq} 版` : "应用日志";
  frag.append(el("div", { class: "log-controls" }, [
    el("h2", { class: "section-title grow", text: title }),
    button(state.logFollow ? "暂停跟随" : "跟随新日志", "btn sm" + (state.logFollow ? " primary" : ""), () => toggleLogFollow(a.name), "日志跟随开关"),
    tail,
    button("刷新", "btn sm", () => refreshLogs(a.name, true), "刷新日志"),
    button("复制给 AI", "btn sm", () => copyToClipboard(logsForAI(a), "已复制，粘贴给你的 AI 即可"), "复制最近日志给 AI"),
  ]));
  frag.append(el("p", { class: "faint", text: (state.logFollow ? "每 3 秒跟随新日志" : "已暂停跟随") + " · 最多保留 2000 行" + (a.live && a.live.seq > 1 ? ` · 当前显示第 ${a.live.seq} 版的日志；更早版本的日志已随旧容器删除。` : "") }));
  if (state.logError) frag.append(el("p", { class: "muted", role: "status", text: state.logError }));
  const pre = el("pre", { class: "logbox", tabindex: "0", role: "log", "aria-label": "应用日志" });
  if (state.logs === null) pre.textContent = "正在加载日志…";
  else if (state.logs.length === 0) pre.textContent = "暂无日志。";
  else pre.textContent = state.logs.join("\n");
  frag.append(pre);
  return frag;
}

function logsForAI(a) {
  const lines = (state.logs || []).slice(-80);
  return `这是我在 AcornFox 上的应用「${a.name}」最近 ${lines.length} 行日志，请帮我分析：\n` + lines.join("\n");
}

function meterNode(label, valueText, percent) {
  const fill = el("span");
  if (percent != null && Number.isFinite(percent)) fill.style.width = Math.min(100, Math.max(0, percent)) + "%";
  const bar = el("div", { class: "bar" + (percent != null && percent >= 80 ? " warn" : "") }, fill);
  return el("div", { class: "meter" }, [el("div", { class: "top" }, [el("span", { text: label }), el("span", { text: valueText })]), bar]);
}

function appMetricsPanel(a) {
  const metrics = state.appMetrics;
  if (!metrics) return el("p", { class: "muted", text: "资源指标：正在采样…" });
  if (metrics.available === false) return el("p", { class: "muted", text: "资源指标暂不可用（" + observedMetricText(metrics.observed_state) + "）。" });
  const cpu = metrics.cpu_available === false ? "采样中" : numberText(metrics.cpu_percent, "%");
  const cpuPct = metrics.cpu_available === false ? null : metrics.cpu_percent;
  const memory = numberText(metrics.memory_usage_mb, " MB");
  const limit = metrics.memory_limit_mb > 0 ? numberText(metrics.memory_limit_mb, " MB") : "未设置内存上限";
  const memPct = metrics.memory_limit_mb > 0 && Number.isFinite(metrics.memory_usage_mb) ? (metrics.memory_usage_mb / metrics.memory_limit_mb) * 100 : null;
  return el("section", { class: "app-metrics", "aria-label": a.name + " 当前资源指标" }, [
    meterNode("CPU（1 核 = 100%）", cpu, cpuPct),
    meterNode("内存", memory + " / " + limit, memPct),
    el("div", { class: "meter" }, el("div", { class: "top" }, [el("span", { text: "网络 接收 / 发送" }), el("span", { text: sizeText(metrics.network_rx_mb) + " / " + sizeText(metrics.network_tx_mb) })])),
    el("div", { class: "meter" }, el("div", { class: "top" }, [el("span", { text: "进程数" }), el("span", { text: Number.isFinite(metrics.pids) ? String(metrics.pids) : "不可用" })])),
  ]);
}

function observedMetricText(s) {
  return ({ stopped: "已停止", exited: "已停止", missing: "尚无容器", restarting: "正在重启", unavailable: "执行器不可用" })[s] || s || "不可用";
}

// sizeText 按大小换单位，KB 级流量也能看见。
function sizeText(mb) {
  if (!Number.isFinite(mb)) return "不可用";
  if (mb >= 1024) return (mb / 1024).toFixed(1) + " GB";
  if (mb >= 1) return mb.toFixed(1) + " MB";
  return Math.round(mb * 1024) + " KB";
}

function numberText(value, unit) {
  return Number.isFinite(value) ? value.toFixed(1) + (unit || "") : "不可用";
}

// ---------------------------------------------------------------------------
// 路由
// ---------------------------------------------------------------------------

function parseHash(h) {
  const raw = (h || "").replace(/^#/, "");
  if (raw.startsWith("app=")) return { page: "app", app: decodeURIComponent(raw.slice(4)), tab: "overview" };
  const parts = raw.replace(/^\//, "").split("/");
  const head = parts[0] || "";
  if (head === "app" && parts[1]) {
    const tab = APP_TABS.some(([k]) => k === parts[2]) ? parts[2] : "overview";
    let name = parts[1];
    try { name = decodeURIComponent(parts[1]); } catch (_) { /* 原样使用 */ }
    return { page: "app", app: name, tab };
  }
  if (["store", "server", "settings", "new"].includes(head)) return { page: head };
  return { page: "home" };
}

function go(hash) {
  if (location.hash === hash) onRoute();
  else location.hash = hash;
}

function onRoute() {
  const r = parseHash(location.hash);
  const prevApp = state.openApp;
  const prevTab = state.detailTab;
  const prevPage = currentPage();
  state.route = r;
  if (r.page === "app") {
    if (r.app !== prevApp) {
      resetLogReader();
      state.openApp = r.app;
      state.detail = null;
      state.appMetrics = null;
      state.deployments = null;
      state.detailTab = r.tab;
      loadDetail(r.app);
      loadDeployments(r.app);
    } else {
      state.detailTab = r.tab;
    }
    if (prevTab === "logs" && r.tab !== "logs") {
      state.logGeneration++;
      if (logRequest) logRequest.abort();
      logRequest = null;
    }
    if (r.tab === "logs" && state.logs === null) refreshLogs(r.app);
    if (r.tab === "versions" || r.tab === "overview") loadDeployments(r.app);
  } else {
    resetLogReader();
    state.openApp = null;
    state.detail = null;
    state.appMetrics = null;
    state.deployments = null;
  }
  if (r.page !== "app" || r.app !== prevApp) state.limitDraft = null;
  render();
  if (needsResources()) loadResources(true);
  // 切换动效：同一应用内换标签，内容按标签先后从右或左滑入；换顶部导航页面时整页同样处理。
  const tabIndex = (k) => APP_TABS.findIndex(([t]) => t === k);
  const nextPage = currentPage();
  if (r.page === "app" && r.app === prevApp && r.tab !== prevTab) {
    playEnter($("page").querySelector('[role="tabpanel"]'), tabIndex(r.tab) > tabIndex(prevTab));
  } else if (nextPage !== prevPage) {
    const pageIndex = (id) => PAGES.findIndex(([p]) => p === id);
    playEnter($("page"), pageIndex(nextPage) > pageIndex(prevPage));
  }
  if (state.focusLimits) {
    state.focusLimits = false;
    const limits = r.page === "app" && r.tab === "config" && $("page").querySelector('[data-region^="limits:"]');
    if (limits) limits.scrollIntoView({ block: "center", behavior: reducedMotion() ? "auto" : "smooth" });
  }
  const key = r.page + ":" + (r.app || "");
  if (key !== prevPageKey) window.scrollTo(0, 0);
  prevPageKey = key;
}

let prevPageKey = "";

function playEnter(node, forward) {
  if (!node) return;
  node.classList.remove("enter-r", "enter-l");
  void node.offsetWidth; // 重新触发动画
  node.classList.add(forward ? "enter-r" : "enter-l");
  node.addEventListener("animationend", () => node.classList.remove("enter-r", "enter-l"), { once: true });
}

// 黑色滑块：顶部导航和应用详情标签共用。ctx 与上次相同时从旧位置滑到选中项；
// 第一次出现、换了应用或窗口尺寸变化时直接就位。导航在手机宽度隐藏时不显示滑块。
const sliders = new Map();

function syncSliders() {
  syncSlider("nav", $("nav"), "nav");
  syncSlider("tabs", $("page").querySelector(".tabs"), state.openApp ? "app:" + state.openApp : null);
}

function syncSlider(key, container, ctx) {
  let s = sliders.get(key);
  if (!s) { s = { node: el("span", { class: "slider", "aria-hidden": "true" }), ctx: null }; sliders.set(key, s); }
  const node = s.node;
  const active = container && container.querySelector('[aria-selected="true"], [aria-current="page"]');
  if (!active || !active.offsetWidth) { node.remove(); s.ctx = null; return; }
  const animate = s.ctx === ctx;
  if (node.parentNode !== container) {
    // 换标签时整个标签栏会重建，横向滚动位置随滑块一起带到新的标签栏
    container.prepend(node);
    if (animate && s.scroll) container.scrollLeft = s.scroll;
    container.addEventListener("scroll", () => { s.scroll = container.scrollLeft; }, { passive: true });
  }
  if (!animate) node.style.transition = "none";
  void node.offsetWidth; // 以当前位置为过渡起点（节点可能刚移进新的容器）
  const box = { left: active.offsetLeft, top: active.offsetTop, width: active.offsetWidth, height: active.offsetHeight };
  const moved = node.style.left !== box.left + "px";
  for (const [k, v] of Object.entries(box)) if (node.style[k] !== v + "px") node.style[k] = v + "px";
  if (!animate) { void node.offsetWidth; node.style.transition = ""; }
  // 手机宽度下标签栏可横向滚动：选中项变了或刚出现时，把它滚进可见范围（用户自己滑动时不打扰）
  if ((moved || !animate) && container.scrollWidth > container.clientWidth) {
    const left = box.left, right = box.left + box.width, viewL = container.scrollLeft, viewR = viewL + container.clientWidth;
    if (left < viewL || right > viewR) {
      s.scroll = Math.max(0, left - (container.clientWidth - box.width) / 2);
      container.scrollTo({ left: s.scroll, behavior: animate && !reducedMotion() ? "smooth" : "auto" });
    }
  }
  s.ctx = ctx;
}

function resetSliders() {
  for (const s of sliders.values()) s.ctx = null;
  syncSliders();
}

// ---------------------------------------------------------------------------
// 交互
// ---------------------------------------------------------------------------

// detailGen 让后发的详情请求优先：保存设置后重新读取时，更早发出的轮询请求可能晚到，
// 不能用它的旧数据覆盖。
let detailGen = 0;

async function loadDetail(name) {
  const gen = ++detailGen;
  try {
    const [d, metrics] = await Promise.all([
      api.get(appPath(name)),
      api.get("/v1/metrics/apps/" + encodeURIComponent(name)).catch(() => ({ available: false, observed_state: "unavailable" })),
    ]);
    if (state.openApp !== name || gen !== detailGen) return;
    state.detail = d;
    state.appMetrics = metrics;
    render();
  } catch (e) {
    if (e.status === 404 && state.openApp === name) { state.detail = null; render(); return; }
    handleError(e);
  }
}

async function loadDeployments(name) {
  try {
    const body = await api.get(appPath(name, "/deployments?limit=20"));
    if (state.openApp !== name) return;
    state.deployments = body.deployments || body.items || [];
    render();
  } catch (e) { if (e.status !== 404) handleError(e); }
}

function resetLogReader() {
  state.logGeneration++;
  if (logRequest) logRequest.abort();
  logRequest = null;
  state.logs = null;
  state.logCursor = "";
  state.logFollow = false;
  state.logError = null;
}

function toggleLogFollow(name) {
  state.logFollow = !state.logFollow;
  if (!state.logFollow) {
    state.logGeneration++;
    if (logRequest) logRequest.abort();
    logRequest = null;
  } else refreshLogs(name);
  render();
}

async function refreshLogs(name, reset = false) {
  if (reset) {
    state.logGeneration++;
    if (logRequest) logRequest.abort();
    logRequest = null;
    state.logCursor = "";
    state.logs = null;
  }
  if (logRequest) return;
  const generation = state.logGeneration;
  const request = new AbortController();
  logRequest = request;
  try {
    const query = new URLSearchParams({ tail: String(state.logTail) });
    if (state.logCursor) query.set("cursor", state.logCursor);
    const body = await api.get(appPath(name, "/log-batch?" + query.toString()), request.signal);
    if (state.openApp !== name || state.logGeneration !== generation) return;
    if (body.reset) state.logs = [];
    state.logs = (state.logs || []).concat(body.lines || []).slice(-2000);
    state.logCursor = body.cursor || "";
    state.logError = null;
    render();
    if (body.has_more && state.logFollow) setTimeout(() => refreshLogs(name), 0);
  } catch (e) {
    if (e.name === "AbortError" || state.openApp !== name || state.logGeneration !== generation) return;
    state.logError = e.message || "无法读取日志";
    if (e.code === "no_live_deployment") state.logs = [];
    if (e.status === 501 || e.status === 404) state.logFollow = false;
    if (e.status === 401) handleError(e);
    else render();
  } finally {
    if (logRequest === request) logRequest = null;
  }
}

async function withBusy(key, fn) {
  if (state.busy[key]) return;
  state.busy[key] = true;
  render();
  try { await fn(); } catch (e) { handleError(e); } finally { delete state.busy[key]; render(); }
}

async function refreshAfterAction(name) {
  await poll();
  if (state.openApp === name) { await loadDetail(name); loadDeployments(name); }
}

async function actOnApp(action, nameArg) {
  const name = nameArg || state.openApp;
  if (!name) return;
  await withBusy(name, async () => {
    await api.post(appPath(name, "/" + action), {});
    toast(({ start: "已提交启动", restart: "已提交重启", redeploy: "已提交重新部署", rollback: "已提交回退，正在部署", stop: "已提交停止" })[action] || "已提交");
    await refreshAfterAction(name);
  });
}

// toggleRun：开关从开到关要确认（默认焦点在“取消”）；从关到开直接启动。
function toggleRun(name, on) {
  if (!on) { actOnApp("start", name); return; }
  showConfirm({
    title: "停止应用",
    body: `确定要停止「${name}」吗？停止后用户将无法访问，直到重新打开开关。数据不会丢失。`,
    ok: "停止",
    action: () => actOnApp("stop", name),
  });
}

function confirmRollback() {
  const name = state.openApp;
  showConfirm({
    title: "回退到上一版",
    body: `「${name}」将用上一版的镜像重新部署，不重新构建。健康检查通过后才切换流量，失败时当前版本继续服务。`,
    ok: "回退",
    action: () => actOnApp("rollback", name),
  });
}

function addAddon(name, kind) {
  withBusy(name + ":" + kind, async () => {
    await api.post(appPath(name, "/addons"), { kind });
    toast(`已添加 ${ADDON_NAMES[kind] || kind}，应用会自动重新部署`);
    await refreshAfterAction(name);
  });
}

function confirmRemoveAddon(name, kind) {
  const label = ADDON_NAMES[kind] || kind;
  showConfirm({
    title: `关闭 ${label}`,
    body: `「${name}」将不再连接 ${label}，应用会自动重新部署。默认保留数据卷，之后重新打开可恢复原来的数据。`,
    ok: "关闭",
    check: "同时删除数据（不可恢复）",
    action: (removeData) => withBusy(name + ":" + kind, async () => {
      await api.del(appPath(name, "/addons/" + encodeURIComponent(kind) + (removeData ? "?volumes=true" : "")));
      toast(removeData ? `已关闭 ${label} 并删除数据` : `已关闭 ${label}，数据已保留`);
      await refreshAfterAction(name);
    }),
  });
}

async function logout() {
  try { await api.post("/v1/console/logout", {}); } catch (_) { /* 忽略 */ }
  state.fatal = { title: "已退出登录", body: "你已退出控制台。", again: true };
  stopPolling();
  render();
}

// ---------------------------------------------------------------------------
// 确认框与“让 AI 帮忙”
// ---------------------------------------------------------------------------

let confirmAction = null;
let lastFocus = null;

// 弹窗开关动效：打开时先显示再加 is-open 触发过渡；关闭时换成 is-closing，
// 等过渡结束再隐藏。时长与 app.css 一致；减少动态效果时立即隐藏。
const LAYER_CLOSE_MS = 150;
const layerTimers = new Map();

function reducedMotion() {
  return !!(window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches);
}

function showLayer(...nodes) {
  for (const n of nodes) {
    clearTimeout(layerTimers.get(n));
    n.classList.remove("is-closing");
    n.hidden = false;
  }
  void nodes[0].offsetWidth; // 先让 hidden 的移除生效，过渡才有起点
  for (const n of nodes) n.classList.add("is-open");
}

function hideLayer(...nodes) {
  for (const n of nodes) {
    if (!layerOpen(n)) continue;
    n.classList.remove("is-open");
    if (reducedMotion()) { n.hidden = true; continue; }
    n.classList.add("is-closing");
    layerTimers.set(n, setTimeout(() => { n.hidden = true; n.classList.remove("is-closing"); }, LAYER_CLOSE_MS));
  }
}

// 正在关闭的弹窗算作已关闭，Esc 会落到下面一层。
function layerOpen(n) {
  return !n.hidden && !n.classList.contains("is-closing");
}

function showConfirm(o) {
  lastFocus = document.activeElement;
  $("confirm-title").textContent = o.title;
  $("confirm-body").textContent = o.body;
  $("confirm-ok").textContent = o.ok;
  $("confirm-ok").className = "btn primary";
  const extra = $("confirm-extra");
  $("confirm-check").checked = false;
  if (o.check) { $("confirm-check-label").textContent = o.check; extra.hidden = false; } else extra.hidden = true;
  confirmAction = o.action;
  showLayer($("confirm-overlay"), $("confirm-dialog"));
  // 默认焦点放在安全选项上：回车不会停止或删除任何东西。
  $("confirm-cancel").focus();
}

function hideConfirm() {
  hideLayer($("confirm-overlay"), $("confirm-dialog"));
  confirmAction = null;
  if (lastFocus && lastFocus.isConnected) lastFocus.focus({ preventScroll: true });
}

function openAskAI() {
  lastFocus = document.activeElement;
  const sheet = $("sheet");
  clear(sheet);
  const items = attentionItems();
  sheet.append(el("h3", { id: "sheet-title", text: "让 AI 帮忙" }));
  sheet.append(el("p", { text: "把这句话发给 Claude Code、Codex 等 AI 工作台，它会装好 AcornFox Skill，之后部署、排错、加数据库都可以一句话完成：" }));
  sheet.append(copyBlock(SKILL_PROMPT, "已复制，发给你的 AI 即可", "复制 Skill 安装提示"));
  if (items.length) {
    const all = items.map((it) => copyForAI(it.diag, it.app, it.deploymentID)).join("\n\n");
    sheet.append(el("p", { text: `现在有 ${items.length} 项需要处理，可以一起交给 AI：` }));
    sheet.append(el("div", {}, button("复制全部问题给 AI", "btn primary", () => copyToClipboard(all, "已复制，粘贴给你的 AI 即可"), "复制全部问题给 AI")));
  }
  sheet.append(el("div", { class: "actions" }, button("关闭", "btn", closeSheet, "关闭")));
  showLayer($("sheet-overlay"), sheet);
  sheet.querySelector("button").focus();
}

function closeSheet() {
  hideLayer($("sheet-overlay"), $("sheet"));
  if (lastFocus && lastFocus.isConnected) lastFocus.focus({ preventScroll: true });
}

// ---------------------------------------------------------------------------
// 剪贴板与提示
// ---------------------------------------------------------------------------

function copyToClipboard(content, okMsg) {
  const done = () => toast(okMsg || "已复制");
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(content).then(done).catch(() => fallbackCopy(content, done));
  } else {
    fallbackCopy(content, done);
  }
}

function fallbackCopy(content, done) {
  const ta = document.createElement("textarea");
  ta.value = content;
  ta.setAttribute("readonly", "");
  ta.className = "sr-only";
  document.body.append(ta);
  ta.select();
  try {
    if (document.execCommand("copy")) done();
    else toast("复制失败，请手动复制");
  } catch (_) { toast("复制失败，请手动复制"); }
  ta.remove();
}

let toastTimer = null;
function toast(msg) {
  const t = $("toast");
  t.textContent = msg;
  t.classList.add("show");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => t.classList.remove("show"), 2200);
}

// ---------------------------------------------------------------------------
// 外观
// ---------------------------------------------------------------------------

function readTheme() {
  try { return localStorage.getItem("af-theme"); } catch (_) { return null; }
}

function themeMode() {
  const saved = readTheme();
  return saved === "dark" || saved === "light" ? saved : "system";
}

function applyTheme() {
  const saved = readTheme();
  if (saved === "dark" || saved === "light") document.documentElement.dataset.theme = saved;
  else delete document.documentElement.dataset.theme;
}

function setTheme(mode) {
  try {
    if (mode === "dark" || mode === "light") localStorage.setItem("af-theme", mode);
    else localStorage.removeItem("af-theme");
  } catch (_) { /* 隐私模式 */ }
  applyTheme();
}

function toggleTheme() {
  const cur = document.documentElement.dataset.theme;
  const prefersDark = window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches;
  const effective = cur || (prefersDark ? "dark" : "light");
  setTheme(effective === "dark" ? "light" : "dark");
}

// ---------------------------------------------------------------------------
// 准备运行环境（主程序装好后在页面里准备 Docker、网关组件、自检）
// 和 `acornfox setup` 读同一个接口：GET /v1/setup、POST /v1/setup/{start,resume,pause}。
// 没准备好时盖一层“准备运行环境”；以前准备好过、开机重新检查时只显示一条横幅。
// ---------------------------------------------------------------------------

const SETUP_STEP_NO = { binary: 1, runtime: 2, edge: 3, checks: 4 };
let setupTimer = null;
let setupChoice = "builtin";
let setupMirrorOpen = false;

function setupVisible() {
  const s = state.setup;
  if (!s || state.fatal) return false;
  if (state.setupDismissed && s.state === "done") return false;
  if (s.state === "done") return state.setupShownRun;  // 这次页面上看过准备过程，才显示“完成”页
  if (["needs_user", "failed", "paused"].includes(s.state)) return true;
  return !s.ever_done;                                  // 运行中 / 没开始：第一次准备才盖整页
}

async function loadSetup() {
  try {
    state.setup = await api.get("/v1/setup");
  } catch (e) {
    if (e.status === 404) { state.setup = null; return; }  // 服务端没有准备流程（旧版本）
    throw e;
  }
  if (setupVisible() && state.setup.state !== "done") state.setupShownRun = true;
  syncSetupTimer();
}

// 准备层显示时每秒刷新一次进度，其余时间跟着 3 秒的轮询走。
function syncSetupTimer() {
  const fast = state.setup && setupVisible() && ["running", "idle"].includes(state.setup.state);
  if (fast && !setupTimer) {
    setupTimer = setInterval(async () => {
      if (document.hidden) return;
      try { await loadSetup(); render(); } catch (e) { handleError(e); }
    }, 1000);
  } else if (!fast && setupTimer) { clearInterval(setupTimer); setupTimer = null; }
}

async function setupAction(path, body) {
  try {
    state.setup = await api.post(path, body || {});
    state.setupShownRun = true;
    state.setupDismissed = false;
    syncSetupTimer();
    render();
  } catch (e) {
    if (e.status === 409) { toast("运行环境正在准备中"); return; }
    handleError(e);
  }
}

function setupMB(b) { return (b / 1048576).toFixed(0) + " MB"; }

function setupIcon(stateName, no) {
  const map = { done: ["ok", "✓"], failed: ["bad", "!"], needs_user: ["now", String(no)], running: ["now", String(no)], paused: ["wait", "‖"] };
  const [cls, t] = map[stateName] || ["wait", String(no)];
  return el("span", { class: "setup-ic " + cls, "aria-hidden": "true", text: t });
}

function setupStepValue(st, s) {
  if (st.state === "done") return el("span", { class: "setup-v", text: st.id === "binary" ? "已装好" : st.id === "checks" ? "通过" : "运行中" });
  if (st.state === "running") {
    const items = st.items || [];
    const done = items.filter((i) => i.state === "done").length;
    return el("span", { class: "pill now", text: items.length > 1 ? "进行中 " + done + "/" + items.length : "进行中" });
  }
  if (st.state === "needs_user") return el("span", { class: "pill now", text: s.need && s.need.kind === "reboot" ? "需要重启" : "需要你确认" });
  if (st.state === "failed") return el("span", { class: "pill bad", text: "没成功 · " + ((s.error && s.error.code) || "") });
  if (st.state === "paused") return el("span", { class: "setup-v", text: "已暂停" });
  return el("span", { class: "setup-v", text: "等待" });
}

function setupItems(items) {
  return el("div", { class: "setup-sub" }, items.map((it) => {
    const right = it.state === "done"
      ? el("span", { class: "setup-r ok", text: "✓ " + (it.total_bytes ? setupMB(it.total_bytes) + " · " : "") + (it.detail || "完成") })
      : el("span", { class: "setup-r", text: it.total_bytes && it.done_bytes ? setupMB(it.done_bytes) + " / " + setupMB(it.total_bytes) : (it.detail || "进行中") });
    const pct = it.state === "done" ? 100 : it.total_bytes ? Math.min(100, Math.round((it.done_bytes || 0) * 100 / it.total_bytes)) : 0;
    const meta = [];
    if (it.state !== "done") {
      if (it.speed_bps) meta.push((it.speed_bps / 1048576).toFixed(1) + " MB/s");
      if (it.eta_s) meta.push("还要约 " + it.eta_s + " 秒");
      if (it.source) meta.push("来源 " + it.source);
    }
    return el("div", { class: "setup-item" }, [
      el("div", { class: "setup-row" }, [el("b", { text: it.title }), right]),
      it.total_bytes || it.state === "done" ? el("div", { class: "bar" + (it.state === "done" ? " done" : "") }, el("span", { style: "width:" + pct + "%" })) : null,
      meta.length ? el("div", { class: "faint", text: meta.join(" · ") }) : null,
    ]);
  }));
}

function setupNeedBox(s) {
  const n = s.need;
  if (n.kind === "choose_runtime") {
    return el("div", { class: "setup-ask", role: "radiogroup", "aria-label": "选择运行环境" }, [
      el("div", { class: "section-title", text: "用哪个？" }),
      ...(n.options || []).map((o) => el("button", {
        class: "setup-box" + (setupChoice === o.id ? " on" : ""), role: "radio", "aria-checked": String(setupChoice === o.id),
        onclick: () => { setupChoice = o.id; render(); },
      }, [el("span", { class: "setup-rd", "aria-hidden": "true" }), el("span", {}, [
        el("span", { class: "section-title", text: o.title }), o.recommended ? el("span", { class: "setup-rec", text: "推荐" }) : null,
        el("div", { class: "faint", text: o.detail }),
      ])])),
    ]);
  }
  return el("div", { class: "setup-warn" }, [
    el("div", { class: "section-title", text: n.kind === "reboot" ? "重启电脑后自动接着装" : "点“继续”后会这样：" }),
    n.steps && n.steps.length ? el("ol", { class: "setup-steps" }, n.steps.map((t) => el("li", { text: t }))) : el("p", { class: "muted", text: n.message }),
  ]);
}

function setupFailBox(s) {
  const f = s.error;
  return el("div", { class: "setup-fix" }, [
    el("p", { text: f.message }),
    f.next ? el("p", { class: "muted", text: "怎么办：" + f.next }) : null,
    f.kept_bytes ? el("p", { class: "faint", text: "已下载的 " + setupMB(f.kept_bytes) + " 会保留，重试从断点接着下。" }) : null,
    el("div", { class: "setup-btns" }, [
      el("button", { class: "btn sm", text: "重试", onclick: () => setupAction("/v1/setup/resume") }),
      el("button", { class: "btn sm", text: "换下载源", onclick: () => { setupMirrorOpen = !setupMirrorOpen; render(); } }),
      el("button", { class: "btn sm", text: "复制诊断信息给 AI", onclick: () => copyToClipboard(
        "AcornFox 准备运行环境失败，请帮我排查。故障编号 " + f.code + "。\n" + JSON.stringify({ error: f, steps: s.steps, found: s.found, network: s.network, version: s.version }, null, 2),
        "已复制，可以发给 AI 工作台") }),
    ]),
    setupMirrorOpen ? setupMirrorForm() : null,
  ]);
}

function setupMirrorForm() {
  const input = el("input", { class: "input", type: "url", placeholder: "下载源地址，例如 https://dl.example.com", "aria-label": "下载源地址" });
  return el("div", { class: "setup-btns" }, [input, el("button", { class: "btn sm primary", text: "用这个源重试", onclick: () => {
    const v = input.value.trim();
    if (!/^https?:\/\//.test(v)) { toast("请输入 http:// 或 https:// 开头的地址"); return; }
    setupMirrorOpen = false;
    setupAction("/v1/setup/start", { mirror: v });
  } })]);
}

function buildSetupSheet(s) {
  const done = s.state === "done";
  const steps = s.steps || [];
  const sub = done ? "运行环境准备好了" + (s.started_at && s.finished_at ? "，用时 " + setupDuration(s) : "") + "。以后开机会自动准备好，不用再管。"
    : s.state === "failed" ? "有一步没成功。已经装好的部分不会丢，重试只补没完成的。"
    : s.state === "needs_user" ? (s.need ? s.need.message : "")
    : s.state === "paused" ? "已暂停，已下载的部分会保留。"
    : "主程序已经装好。正在准备运行应用需要的环境，完成前应用暂时不能部署。";
  const rows = [];
  for (const st of steps) {
    rows.push(el("div", { class: "setup-gr" }, [
      setupIcon(st.state, SETUP_STEP_NO[st.id]),
      el("div", {}, [el("div", { class: "section-title", text: st.title }), st.detail ? el("div", { class: "faint", text: st.detail }) : null]),
      setupStepValue(st, s),
    ]));
    if (st.state === "running" && (st.items || []).length) rows.push(setupItems(st.items));
    if (st.state === "needs_user" && s.need) rows.push(setupNeedBox(s));
    if (st.state === "failed" && s.error) rows.push(setupFailBox(s));
  }
  const checks = (s.checks || []).length && (done || s.state === "failed") ? el("div", {}, [
    el("div", { class: "setup-ghead", text: "自检 " + s.checks.filter((c) => c.ok).length + "/" + s.checks.length + " 通过" }),
    el("div", { class: "setup-group" }, s.checks.map((c) => el("div", { class: "setup-gr" }, [
      el("span", { class: "setup-ic " + (c.ok ? "ok" : c.required ? "bad" : "wait"), "aria-hidden": "true", text: c.ok ? "✓" : "!" }),
      el("div", {}, [el("div", { class: "section-title", text: c.title }), c.detail ? el("div", { class: "faint mono", text: c.detail }) : null]),
    ]))),
  ]) : null;

  const foot = [];
  if (done) {
    foot.push(el("a", { class: "btn primary", href: "#/store", text: "去应用中心", onclick: () => { state.setupDismissed = true; } }));
    foot.push(el("a", { class: "btn", href: "#/new", text: "部署我自己的项目", onclick: () => { state.setupDismissed = true; } }));
    foot.push(el("span", { class: "faint setup-hint", text: "也可以在 AI 工作台里说“帮我部署”" }));
  } else if (s.state === "needs_user" && s.need && s.need.kind === "choose_runtime") {
    foot.push(el("button", { class: "btn primary", text: "继续", onclick: () => setupAction("/v1/setup/start", { runtime: setupChoice }) }));
    foot.push(el("span", { class: "faint setup-hint", text: "终端：acornfox setup --runtime " + setupChoice }));
  } else if (s.state === "needs_user" && s.need && s.need.kind === "admin") {
    foot.push(el("button", { class: "btn primary", text: "继续（需要管理员确认）", onclick: () => setupAction("/v1/setup/start", { runtime: "builtin", confirm_admin: true }) }));
    if ((s.need.options || []).some((o) => o.id === "local-only")) {
      foot.push(el("button", { class: "btn", text: "只在本机用", onclick: () => setupAction("/v1/setup/start", { runtime: "builtin", local_only: true }) }));
    } else {
      foot.push(el("span", { class: "faint setup-hint", text: "已经装了 Docker Desktop？打开它后点“重新检测”" }));
      foot.push(el("button", { class: "btn", text: "重新检测", onclick: () => setupAction("/v1/setup/start", {}) }));
    }
  } else if (s.state === "needs_user") {
    foot.push(el("button", { class: "btn", text: "已经重启过了，接着装", onclick: () => setupAction("/v1/setup/resume") }));
  } else if (s.state === "running" || s.state === "idle") {
    foot.push(el("button", { class: "btn", text: "暂停", onclick: () => setupAction("/v1/setup/pause") }));
    foot.push(el("span", { class: "faint setup-hint", text: "关掉这个页面也会在后台继续" }));
  } else if (s.state === "paused") {
    foot.push(el("button", { class: "btn primary", text: "继续", onclick: () => setupAction("/v1/setup/resume") }));
  } else if (s.state === "failed") {
    foot.push(el("span", { class: "faint setup-hint", text: "终端里重试：acornfox setup --resume · 详细诊断：acornfox doctor" }));
  }
  return el("section", { class: "setup-sheet", role: "dialog", "aria-modal": "true", "aria-labelledby": "setup-title" }, [
    el("div", { class: "setup-head" }, [
      el("div", { class: "setup-sq" + (done ? " ok" : s.state === "failed" ? " bad" : ""), "aria-hidden": "true", text: done ? "✓" : s.state === "failed" ? "!" : "…" }),
      el("div", {}, [el("h1", { id: "setup-title", text: done ? "可以部署应用了" : "准备运行环境" }), el("div", { class: "muted", text: sub })]),
    ]),
    el("div", { class: "setup-body" }, [el("div", { class: "setup-group" }, rows), checks, el("div", { class: "setup-foot" }, foot)]),
  ]);
}

function setupDuration(s) {
  const secs = Math.max(0, Math.round((Date.parse(s.finished_at) - Date.parse(s.started_at)) / 1000));
  return secs >= 60 ? Math.floor(secs / 60) + " 分 " + (secs % 60) + " 秒" : secs + " 秒";
}

// 准备层盖在页面上；以前准备好过、开机重新检查时只出一条横幅。
function renderSetupLayer() {
  const existing = $("setup-layer");
  const s = state.setup;
  const banner = $("setup-banner");
  const showBanner = s && !setupVisible() && s.ever_done && s.state === "running" && !state.fatal;
  if (!showBanner && banner) banner.remove();
  if (showBanner && !banner) {
    document.body.append(el("div", { id: "setup-banner", class: "banner", role: "status" }, el("div", { class: "inner", text: "正在检查运行环境…" })));
  }
  if (!setupVisible()) { if (existing) existing.remove(); return; }
  const layer = el("div", { id: "setup-layer", class: "setup-layer" }, buildSetupSheet(s));
  if (existing) existing.replaceWith(layer); else document.body.append(layer);
}

// ---------------------------------------------------------------------------
// 轮询与错误
// ---------------------------------------------------------------------------

async function poll() {
  if (pollInFlight) return;
  pollInFlight = true;
  try {
    const [appsBody, hostBody, statusBody] = await Promise.all([
      api.get("/v1/apps"),
      api.get("/v1/host").catch(() => ({ available: false })),
      api.get("/v1/status").catch(() => ({})),
    ]);
    state.apps = appsBody.apps || [];
    state.host = hostBody;
    state.edge = statusBody.edge || null;
    if (statusBody.setup && (statusBody.setup.state !== "done" || (state.setup && state.setup.state !== "done"))) await loadSetup();
    else if (statusBody.setup && !state.setup) await loadSetup();
    if (needsResources()) await loadResources(false);
    state.loaded = true;
    state.connectionLost = false;
    if (state.fatal && state.fatal.code === "connection") state.fatal = null;
    render();
    checkNewAlerts();
    // 记下应用名：等待详情返回期间用户可能已离开详情页，state.openApp 会变成 null。
    const name = state.openApp;
    if (name) {
      await loadDetail(name);
      if (state.openApp !== name) return;
      if (state.detailTab === "overview" || state.detailTab === "versions") loadDeployments(name);
      if (state.detailTab === "logs" && state.logFollow) await refreshLogs(name);
    }
  } catch (e) { handleError(e); }
  finally { pollInFlight = false; }
}

function handleError(e) {
  if (e.status === 401 && e.code === "session_expired") {
    state.fatal = { title: "会话已过期", body: "为了安全，登录会话已过期。", again: true };
    stopPolling();
    render();
    return;
  }
  if (e.status === 401) {
    state.fatal = { title: "需要重新登录", body: "登录状态已失效。", again: true };
    stopPolling();
    render();
    return;
  }
  if (typeof e.status === "undefined") {
    state.connectionLost = true;
    render();
    return;
  }
  toast("操作失败：" + (e.message || "服务器错误"));
}

function startPolling() {
  stopPolling();
  pollTimer = setInterval(() => { if (!document.hidden) poll(); }, POLL_MS);
}

function stopPolling() {
  if (pollTimer) { clearInterval(pollTimer); pollTimer = null; }
}

// ---------------------------------------------------------------------------
// 启动
// ---------------------------------------------------------------------------

async function boot() {
  applyTheme();
  $("theme").addEventListener("click", () => { toggleTheme(); render(); });
  $("ask-ai").addEventListener("click", openAskAI);
  $("bell").addEventListener("click", (e) => { e.stopPropagation(); toggleNotify(); });
  // 点面板外关闭；卡片悬停时不自动收起
  document.addEventListener("click", (e) => { if (layerOpen($("notify")) && !$("notify").contains(e.target)) closeNotify(); });
  $("notify-pop").addEventListener("mouseenter", () => clearTimeout(popTimer));
  $("notify-pop").addEventListener("mouseleave", () => { popTimer = setTimeout(hidePop, 3000); });
  $("sheet-overlay").addEventListener("click", closeSheet);
  $("confirm-cancel").addEventListener("click", hideConfirm);
  $("confirm-overlay").addEventListener("click", hideConfirm);
  $("confirm-ok").addEventListener("click", async () => {
    const action = confirmAction;
    const checked = $("confirm-check").checked;
    hideConfirm();
    if (action) await action(checked);
  });
  document.addEventListener("keydown", (e) => {
    if (e.key !== "Escape") return;
    if (layerOpen($("confirm-dialog"))) { hideConfirm(); return; }
    if (layerOpen($("notify"))) { closeNotify(); return; }
    if (layerOpen($("notify-pop"))) { hidePop(); return; }
    if (layerOpen($("sheet"))) closeSheet();
  });
  document.addEventListener("visibilitychange", () => { if (!document.hidden) poll(); });
  window.addEventListener("hashchange", onRoute);
  window.addEventListener("resize", resetSliders);
  if (document.fonts && document.fonts.ready) document.fonts.ready.then(resetSliders);
  if (window.matchMedia) {
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    if (mq.addEventListener) mq.addEventListener("change", () => render());
  }

  onRoute();

  try {
    const sess = await api.session();
    if (sess && sess.server) {
      state.server = sess.server;
      document.title = "AcornFox · " + sess.server;
    }
  } catch (e) {
    if (e.status === 401) { handleError(e); return; }
    // 开发环境没有会话接口时，不影响只读轮询。
  }
  await poll();
  startPolling();
}

boot();

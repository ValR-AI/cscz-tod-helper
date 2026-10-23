'use strict';
// CS:CZ ToD Helper interface. All game rules live in the Go server: the page
// sends the draft savestate to /api/evaluate and renders what comes back.

const $ = (sel, el = document) => el.querySelector(sel);
const $$ = (sel, el = document) => [...el.querySelectorAll(sel)];
const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const DIFFS = ['easy', 'normal', 'hard', 'expert'];
const DIFF_LABEL = { easy: 'Easy', normal: 'Normal', hard: 'Hard', expert: 'Expert' };

const S = {
  app: null,        // /api/state
  original: null,   // name of the savestate being edited, null for a new one
  draft: null,
  ev: null,         // last evaluation of the draft
  dirty: false,
  evalSeq: 0,
  as: { since: 0, events: [], status: null },
  profiles: [],
  detail: null,     // operator shown in the detail panel
  side: 'ct',       // operator list shown: ct (hire) or t (terrorists, read only)
  terrorists: [],   // /api/terrorists
};

async function api(path, body) {
  const opts = body === undefined ? {} : {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-ToD-Helper': '1' },
    body: JSON.stringify(body),
  };
  let res;
  try {
    res = await fetch(path, opts);
  } catch {
    throw new Error('The helper is not running anymore. Start it again, then reload this page.');
  }
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    const err = new Error(data.error || res.statusText);
    err.status = res.status;
    err.data = data;
    throw err;
  }
  return data;
}

// ---------------------------------------------------------------------------
// Feedback
// ---------------------------------------------------------------------------

let toastTimer;
function toast(msg, bad = false) {
  const t = $('#toast');
  t.textContent = msg;
  t.classList.toggle('bad', bad);
  t.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { t.hidden = true; }, bad ? 7000 : 4500);
}
const showError = (e) => toast(e.message || String(e), true);

// ask() shows a dialog; resolves to the input text (or '') on OK, null on cancel.
function ask({ title, text = '', input, ok = 'OK', danger = false }) {
  const dlg = $('#dialog');
  $('#dialog-title').textContent = title;
  $('#dialog-text').textContent = text;
  const field = $('#dialog-input');
  field.hidden = input === undefined;
  field.value = input ?? '';
  const okBtn = $('#dialog-ok');
  okBtn.textContent = ok;
  okBtn.classList.toggle('danger', danger);
  return new Promise((resolve) => {
    const onKey = (e) => {
      if (e.key === 'Enter' && !field.hidden) { e.preventDefault(); dlg.close('ok'); }
    };
    field.addEventListener('keydown', onKey);
    dlg.addEventListener('close', () => {
      field.removeEventListener('keydown', onKey);
      resolve(dlg.returnValue === 'ok' ? (field.hidden ? '' : field.value.trim()) : null);
    }, { once: true });
    dlg.returnValue = '';
    dlg.showModal();
    (field.hidden ? okBtn : field).focus();
    if (!field.hidden) field.select();
  });
}

// formDialog shows a dialog built from HTML. validate(dialog) returns an
// error text or ''; submit(dialog) runs on OK and may throw to keep the dialog open.
function formDialog({ title, body, ok = 'OK', onInput, submit }) {
  const dlg = $('#form-dialog');
  $('#form-title').textContent = title;
  $('#form-body').innerHTML = body;
  $('#form-ok').textContent = ok;
  const err = $('#form-error');
  err.hidden = true;
  const refresh = () => { if (onInput) onInput(dlg); };
  dlg.addEventListener('input', refresh);
  dlg.addEventListener('change', refresh);
  refresh();
  return new Promise((resolve) => {
    const okBtn = $('#form-ok');
    const onOk = async (e) => {
      e.preventDefault();
      okBtn.disabled = true;
      try {
        const r = await submit(dlg);
        if (r === undefined) return; // stays open (submit showed something)
        close(r);
      } catch (x) {
        err.textContent = x.message || String(x);
        err.hidden = false;
      } finally {
        okBtn.disabled = false;
      }
    };
    // Enter means OK (the first button of the form is Cancel).
    const onKey = (e) => { if (e.key === 'Enter' && !okBtn.disabled) { e.preventDefault(); okBtn.click(); } };
    const close = (r) => {
      okBtn.removeEventListener('click', onOk);
      dlg.removeEventListener('keydown', onKey);
      dlg.removeEventListener('input', refresh);
      dlg.removeEventListener('change', refresh);
      if (dlg.open) dlg.close();
      resolve(r);
    };
    okBtn.addEventListener('click', onOk);
    dlg.addEventListener('keydown', onKey);
    dlg.addEventListener('close', () => close(null), { once: true });
    dlg.showModal();
    const first = $('input:not([disabled]), select', dlg);
    if (first) first.focus();
  });
}

async function confirmDiscard() {
  if (!S.dirty) return true;
  return (await ask({ title: 'Discard your changes?', text: 'The savestate you are editing has unsaved changes.', ok: 'Discard', danger: true })) !== null;
}

// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------

async function reloadState(state) {
  S.app = state || await api('/api/state');
  renderTop();
  renderList();
  renderSettings();
}

// refreshState follows changes made outside the page (the game saving a
// profile, another tab) without touching the savestate being edited.
let refreshing = false;
async function refreshState() {
  if (refreshing || !S.app) return;
  refreshing = true;
  try {
    const st = await api('/api/state');
    if (st.active !== S.app.active) return; // a bundle switch is in progress
    await reloadState(st);
  } catch { /* the helper may be restarting */ } finally {
    refreshing = false;
  }
}

function openFirstOrNew() {
  const list = S.app.savestates;
  if (list.length) return openSavestate(list[0].savestate.name, true);
  return newSavestate(true);
}

async function openSavestate(name, force = false) {
  if (!force && !(await confirmDiscard())) return;
  const v = S.app.savestates.find((x) => x.savestate.name === name);
  if (!v) return newSavestate(true);
  S.original = v.savestate.name;
  S.draft = structuredClone(v.savestate);
  S.dirty = false;
  $('#f-name').value = S.draft.name;
  await evaluate();
  renderList();
}

async function newSavestate(force = false) {
  if (!force && !(await confirmDiscard())) return;
  S.original = null;
  S.draft = { name: '', difficulty: 'easy', allDifficulties: false, beaten: [], current: '', team: [], readOnly: true, cheat: false };
  S.dirty = false;
  $('#f-name').value = '';
  await evaluate();
  renderList();
}

async function evaluate() {
  const seq = ++S.evalSeq;
  // The name field is the only source of truth for the name: an answer that
  // arrives while the user types must not bring an old name back.
  S.draft.name = $('#f-name').value;
  const ev = await api('/api/evaluate', S.draft);
  if (seq !== S.evalSeq) return; // a newer change is on its way
  for (const k of ['notices', 'problems', 'maps', 'teammates', 'tiers']) ev[k] = ev[k] || [];
  ev.savestate.beaten = ev.savestate.beaten || [];
  ev.savestate.team = ev.savestate.team || [];
  S.ev = ev;
  S.draft = ev.savestate;
  S.draft.name = $('#f-name').value;
  renderEditor();
}

function change(mutate) {
  mutate(S.draft);
  S.dirty = true;
  evaluate().catch(showError);
}

// ---------------------------------------------------------------------------
// Rendering: top bar, list, settings
// ---------------------------------------------------------------------------

function renderTop() {
  const sel = $('#bundle-select');
  const options = S.app.bundles.map((b) => `<option ${b === S.app.active ? 'selected' : ''}>${esc(b)}</option>`).join('');
  if (sel.dataset.options !== options) { // do not close an open dropdown for nothing
    sel.innerHTML = options;
    sel.dataset.options = options;
  }
  sel.value = S.app.active;
  const n = S.app.savestates.length;
  const reset = $('#reset-btn');
  $('#reset-sub').textContent = n ? `${n} savestate${n > 1 ? 's' : ''} in ${S.app.active}` : 'bundle is empty';
  reset.disabled = n === 0 || !!S.app.saveDirError;
  $('#setup-banner').hidden = !S.app.saveDirError;
}

function stateLine(v) {
  switch (v.status.state) {
    case 'ready': return ['ready', 'Ready in the Save folder'];
    case 'changed': return ['changed', `Changed by the game (${v.status.beaten} won): reset to restore`];
    case 'missing': return ['missing', 'Not in the Save folder: reset to write it'];
    default: return ['', ''];
  }
}

function renderList() {
  const list = S.app.savestates;
  $('#list-empty').hidden = list.length > 0;
  $('#savestate-list').innerHTML = list.map((v) => {
    const s = v.savestate;
    const active = S.original !== null && s.name === S.original;
    const [cls, txt] = stateLine(v);
    const progress = s.allDifficulties ? 'Full save, all difficulties' : `${DIFF_LABEL[s.difficulty] || s.difficulty} · ${v.beaten}/18 won`;
    const start = (s.current || '').replace(/^(de|cs)_/, '').replace(/_cz$/, '');
    const team = (s.team || []).map((n) => {
      const m = S.ev?.teammates.find((t) => t.name === n);
      return `<img src="img/models/${m ? m.skin : 0}.png" alt="${esc(n)}" title="${esc(n)}">`;
    }).join('');
    return `<li><button type="button" class="ss ${active ? 'active' : ''}" data-open="${esc(s.name)}">
      <span class="ss-top"><span class="ss-name">${esc(s.name)}</span>${s.readOnly ? '<img class="ss-lock" src="img/lock.png" alt="read-only" title="Read-only">' : ''}</span>
      <span class="ss-meta">${esc(progress)} · start ${esc(start)}</span>
      <span class="ss-team">${team}</span>
      ${txt ? `<span class="ss-state ${cls}">${esc(txt)}</span>` : ''}
    </button></li>`;
  }).join('');
}

function renderSettings() {
  const input = $('#save-dir');
  if (document.activeElement !== input) input.value = S.app.settings.saveDir || '';
  const st = $('#save-dir-status');
  if (S.app.saveDirError) {
    st.textContent = S.app.settings.saveDir ? S.app.saveDirError : 'Not set yet.';
    st.className = 'status-line bad';
  } else {
    st.textContent = 'Folder found. Savestates are written here.';
    st.className = 'status-line good';
  }
  const ls = $('#ls-addr');
  if (document.activeElement !== ls) ls.value = S.app.settings.liveSplitAddr || '';
  const kind = S.app.settings.timerKind || 'livesplit';
  $$('#timer-kind button').forEach((b) => b.setAttribute('aria-checked', String(b.dataset.timer === kind)));
  $('#timer-livesplit').hidden = kind !== 'livesplit';
  $('#timer-livesplit-one').hidden = kind !== 'livesplit-one';
  $('#lso-url').textContent = S.app.liveSplitOneURL;
  $('#about-version').textContent = S.app.version;
  $('#about-data').textContent = S.app.dataDir;
}

// ---------------------------------------------------------------------------
// Rendering: editor
// ---------------------------------------------------------------------------

function renderEditor() {
  const ev = S.ev;
  const d = S.draft;
  const nameInput = $('#f-name');
  if (document.activeElement !== nameInput && nameInput.value !== d.name) nameInput.value = d.name;
  renderFileName();

  $$('#f-diff button').forEach((b) => b.setAttribute('aria-checked', String(b.dataset.diff === d.difficulty)));
  $('#f-ro').checked = !!d.readOnly;
  $('#f-all').checked = !!d.allDifficulties;
  $('#f-cheat').checked = !!d.cheat;
  if (d.cheat) $('#advanced').open = true;

  // Stats
  const tierBadges = ev.tiers.map((t) => {
    const tip = t.never ? `Tier ${t.cost}: never unlocked on ${DIFF_LABEL[d.difficulty]}`
      : t.unlockPoints <= 0 ? `Tier ${t.cost}: available from the start`
      : `Tier ${t.cost}: unlocks at ${t.unlockPoints} points (${t.unlockMaps} maps won)`;
    return `<span class="tierbadge ${t.unlocked ? '' : 'locked'}" title="${esc(tip)}"><img src="img/tiers/${t.cost}.png" alt="${esc(tip)}"></span>`;
  }).join('');
  const nextTier = ev.tiers.find((t) => !t.unlocked && !t.never);
  const won = d.allDifficulties ? '18/18 ×4' : `${ev.beatenCount}/18`;
  $('#stats').innerHTML = `
    <div class="stat"><b>${ev.points}</b><span>Reputation points</span></div>
    <div class="stat"><b>${won}</b><span>Maps won</span></div>
    <div class="stat"><b>${d.team.length}/${ev.maxTeam}</b><span>Team slots</span></div>
    <div class="stat ${ev.teamCost > ev.points ? 'over' : ''}"><b>${ev.teamCost}</b><span>Team cost</span></div>
    <div class="stat tiers"><div class="badges">${tierBadges}</div><span>${nextTier ? `Tier ${nextTier.cost} at ${nextTier.unlockPoints} pts` : 'Tiers unlocked'}</span></div>`;

  const nameProblems = (p) => /profile name|not allowed in a profile name/.test(p);
  const problems = ev.problems.filter((p) => !nameProblems(p)); // the name is checked live under the field
  $('#notices').innerHTML = [
    ...problems.map((p) => `<li class="problem">${esc(p)}</li>`),
    ...ev.notices.map((n) => `<li>${esc(n)}</li>`),
  ].join('');

  renderTours();
  renderTeam();

  $('#dup-btn').hidden = S.original === null;
  $('#del-btn').hidden = S.original === null;
  const blocked = problems.length > 0 || !!S.app.saveDirError || !nameOK();
  $('#save-btn').disabled = blocked;
  $('#save-btn').textContent = S.original === null ? 'Save savestate' : (S.dirty ? 'Save changes' : 'Saved');
  $('#editor-status').textContent = S.app.saveDirError ? 'Set your Save folder in Settings to save.'
    : S.dirty ? 'Unsaved changes' : S.original === null ? 'New savestate' : `In bundle ${S.app.active}`;
}

function nameOK() {
  const name = $('#f-name').value.trim();
  return name !== '' && checkNameLocally(name) === '';
}

function renderFileName() {
  const name = $('#f-name').value.trim();
  const el = $('#f-file');
  const bad = name === '' ? '' : checkNameLocally(name);
  el.className = bad ? 'mono bad' : 'mono';
  el.textContent = name === '' ? 'Profile file: CZProfile-<name>.czp' : bad || `Writes ${gameFileName(name)}`;
}

// Same rules as the Go side (savestate.CheckName and FileName).
const NAME_SYMBOLS = "_-()[].!@#$^&*{}|:'<,>";
function checkNameLocally(name) {
  if (name.length > 31) return 'Too long: 31 characters max';
  const badChar = [...name].find((c) => !/[A-Za-z0-9]/.test(c) && !NAME_SYMBOLS.includes(c));
  if (badChar === undefined) return '';
  const what = badChar === ' ' ? 'A space' : `"${badChar}"`;
  return `${what} is not allowed: letters, digits and ${[...NAME_SYMBOLS].join(' ')}`;
}

// gameFileName names the file the way the game does: every character other
// than a letter, a digit, _ or - becomes _.
function gameFileName(name) {
  return `CZProfile-${name.slice(0, 31).replace(/[^A-Za-z0-9_-]/g, '_')}.czp`;
}

function renderTours() {
  const ev = S.ev;
  const d = S.draft;
  let html = '';
  for (let t = 1; t <= 6; t++) {
    const maps = ev.maps.filter((m) => m.tour === t);
    const locked = !maps.some((m) => m.playable);
    html += `<div class="tour ${locked ? 'locked' : ''}"><div class="tour-label"><span>Tour</span><b>${t}</b></div>`;
    for (const m of maps) {
      const cls = [m.beaten ? 'won' : '', m.playable ? '' : 'locked', m.current ? 'current' : ''].join(' ');
      const canToggle = m.playable && !d.allDifficulties;
      const tip = !m.playable ? `Locked: finish tour ${t - 1} first` : d.allDifficulties ? 'Full save: every map is won'
        : m.beaten ? 'Won. Click to mark it not won' : 'Click to mark it won';
      html += `<div class="tile ${cls}">
        <button type="button" class="tile-main" data-map="${m.id}" aria-pressed="${m.beaten}" ${canToggle ? '' : 'aria-disabled="true"'} title="${esc(tip)}">
          <img class="thumb" src="img/maps/${m.id}.png" alt="">
          ${m.beaten ? '<span class="tile-won">WON</span>' : ''}
          ${!m.playable ? '<img class="tile-lock" src="img/lock.png" alt="locked">'
            : m.beaten ? '' : `<span class="tile-enemies" title="Enemies if you play it with ${ev.points} points">${m.enemies} enemies</span>`}
          <span class="tile-name">${esc(m.name)}</span>
        </button>
        <button type="button" class="tile-start" data-start="${m.id}" aria-pressed="${m.current}" ${m.playable ? '' : 'disabled'}>${m.current ? 'Start ✓' : 'Start'}</button>
      </div>`;
    }
    html += '</div>';
  }
  $('#tours').innerHTML = html;

  const cur = ev.maps.find((m) => m.current);
  const tasks = cur.tasks.length ? `<ul>${cur.tasks.map((x) => `<li>${esc(x)}</li>`).join('')}</ul>` : '<p class="hint">No task.</p>';
  const lineup = cur.lineup || [];
  const present = lineup.filter((e) => e.present);
  const later = lineup.filter((e) => !e.present && !e.repeated);
  const repeated = [...new Set(lineup.filter((e) => e.repeated).map((e) => e.name))];
  $('#map-detail').innerHTML = `<h4>Preselected: ${esc(cur.name)} · Tour ${cur.tour}</h4>
    <p>${cur.enemies} terrorist${cur.enemies > 1 ? 's' : ''} at ${ev.points} points, in the order they join:</p>
    <div class="lineup">${present.map(foeChip).join('')}</div>
    ${later.length ? `<p class="hint">With more points:</p><div class="lineup later">${later.map(foeChip).join('')}</div>` : ''}
    ${repeated.length ? `<p class="hint">${esc(repeated.join(', '))} is listed twice for this map in the game files. A name can only be in the game once, so the second one never plays.</p>` : ''}
    <p>Tasks:</p>${tasks}`;
}

const num = (v) => String(Math.round(v * 100) / 100);
const triple = (t) => `${num(t.skill)}/${num(t.aggression)}/${num(t.teamwork)}`;
const times = (t) => `${num(t.reactionTime)}/${num(t.attackDelay)}`;
const weaponOf = (t) => t.weapon || 'random weapon';

function foeChip(e) {
  const t = S.terrorists.find((x) => x.name === e.name);
  const tip = t ? `${t.name}: ${weaponOf(t)} · skill/aggression/teamwork ${triple(t)} · RT/AD ${times(t)} s` : e.name;
  return `<span class="foe-chip ${e.present ? '' : 'later'}" title="${esc(tip)}">
    <img src="img/models/t${t ? t.skin : 0}.png" alt=""><b>${esc(e.name)}</b>${e.present ? '' : `<small>${e.joinsAt} pts</small>`}</span>`;
}

const STAT_HELP = {
  skill: 'Aim and overall ability.',
  aggression: 'How readily it pushes and takes fights.',
  teamwork: 'How much it sticks with you and follows orders.',
  reaction: 'Time before it notices an enemy, and again when an enemy it lost from sight for 0.25 s comes back.',
  delay: 'Once it picks a new target, time before it may shoot it.',
};

// cardText is the three lines of an operator card.
function cardText(t) {
  return `<span class="mate-text"><b>${esc(t.name)}</b>
    <small>${esc(weaponOf(t))} · <span title="Skill / Aggression / Teamwork">${triple(t)}</span></small>
    <small title="Reaction time / Attack delay, in seconds">RT/AD ${times(t)}</small></span>`;
}

function renderTeam() {
  const ev = S.ev;
  const d = S.draft;
  const byName = Object.fromEntries(ev.teammates.map((t) => [t.name, t]));
  const left = ev.teamBudget - ev.teamCost;
  $('#budget').innerHTML = `
    <div class="stat"><b>${ev.teamCost}</b><span>Points spent</span></div>
    <div class="stat ${left < 0 ? 'over' : ''}"><b>${left}</b><span>Points left</span></div>
    <div class="stat"><b>${d.team.length}/${ev.maxTeam}</b><span>Slots used</span></div>
    ${d.allDifficulties ? '<p class="hint">Full save: the team must fit in every difficulty, so the lowest points count.</p>' : ''}`;

  let slots = '';
  for (let i = 0; i < 5; i++) {
    const name = d.team[i];
    if (name) {
      const t = byName[name];
      slots += `<div class="slot filled"><button type="button" data-mate="${esc(name)}" title="Click to remove ${esc(name)}">
        <img src="img/models/${t.skin}.png" alt=""><span><b>${esc(name)}</b><small>Tier ${t.cost} · ${esc(weaponOf(t))}</small></span></button></div>`;
    } else if (i < ev.maxTeam) {
      slots += '<div class="slot">Free slot</div>';
    } else {
      slots += `<div class="slot closed">Opens at ${i + 1} points</div>`;
    }
  }
  $('#hired').innerHTML = slots;

  $$('#side button').forEach((b) => b.setAttribute('aria-checked', String(b.dataset.side === S.side)));
  if (S.side === 't') {
    renderTerrorists();
    renderMateDetail();
    return;
  }
  $('#pool-legend').textContent = 'Cards: first weapon a Counter-Terrorist can buy · skill/aggression/teamwork · RT/AD = reaction time/attack delay (s).';
  let pool = '';
  for (const tier of ev.tiers) {
    const mates = ev.teammates.filter((t) => t.cost === tier.cost);
    const state = tier.unlocked ? (tier.unlockPoints <= 0 ? 'available from the start' : `unlocked at ${tier.unlockPoints} points`)
      : tier.never ? `never unlocked on ${DIFF_LABEL[d.difficulty]}`
      : `unlocks at ${tier.unlockPoints} points (${tier.unlockMaps} maps won)`;
    pool += `<div class="tier ${tier.unlocked ? '' : 'locked'}"><div class="tier-head"><img src="img/tiers/${tier.cost}.png" alt="">
      <b>Tier ${tier.cost}</b><span>Costs ${tier.cost} point${tier.cost > 1 ? 's' : ''} · ${state}</span></div><div class="mates">`;
    for (const t of mates) {
      const cls = [t.selected ? 'selected' : '', t.selectable ? '' : 'blocked', t.tierLocked ? 'cheat' : '', S.detail === t.name ? 'focused' : ''].join(' ');
      const tip = t.selected ? `Hired. Click to remove ${t.name}` : t.selectable ? `Hire ${t.name}` : t.reason;
      pool += `<button type="button" class="mate ${cls}" data-mate="${esc(t.name)}" aria-pressed="${t.selected}" ${t.selectable ? '' : 'aria-disabled="true"'} title="${esc(tip)}">
        <img src="img/models/${t.skin}.png" alt="">${cardText(t)}</button>`;
    }
    pool += '</div></div>';
  }
  $('#pool').innerHTML = pool;
  renderMateDetail();
}

// renderTerrorists shows the enemies with the same cards, grouped by skill
// bracket. They cannot be hired: the cards only open their sheet.
function renderTerrorists() {
  $('#pool-legend').textContent = 'The terrorists of the career, by skill bracket. Cards: first weapon a terrorist can buy · skill/aggression/teamwork · RT/AD = reaction time/attack delay (s).';
  const levels = [...new Set(S.terrorists.map((t) => t.level))];
  let pool = '';
  for (const level of levels) {
    const foes = S.terrorists.filter((t) => t.level === level);
    const diffs = [...new Set(foes.flatMap((t) => t.facedOn))];
    const on = diffs.length ? `faced on ${diffs.join(', ')}` : 'never faced in the career';
    pool += `<div class="tier foes"><div class="tier-head"><b>${esc(level)}</b><span>Skill ${num(foes[0].skill)} · ${esc(on)}</span></div><div class="mates">`;
    for (const t of foes) {
      pool += `<button type="button" class="mate foe ${S.detail === t.name ? 'focused' : ''}" data-foe="${esc(t.name)}" title="${esc(t.name)}: point at it to see the sheet">
        <img src="img/models/t${t.skin}.png" alt="">${cardText(t)}</button>`;
    }
    pool += '</div></div>';
  }
  $('#pool').innerHTML = pool || '<p class="hint">Terrorist data not loaded.</p>';
}

function bar(label, value, help) {
  return `<div class="bar" title="${esc(help)}"><span class="bar-label">${label}</span>
    <span class="bar-track"><span class="bar-fill" style="width:${Math.max(0, Math.min(100, value))}%"></span></span>
    <b>${value}</b><small>${esc(help)}</small></div>`;
}

function renderMateDetail() {
  const el = $('#mate-detail');
  const foe = S.side === 't';
  const t = foe ? S.terrorists.find((x) => x.name === S.detail) : S.ev.teammates.find((x) => x.name === S.detail);
  if (!t) {
    el.innerHTML = `<p class="hint">Point at a ${foe ? 'terrorist' : 'teammate'} to see their stats and the weapons they buy.</p>`;
    return;
  }
  const secs = (v) => `${num(v)} s`;
  const other = foe ? 'CT only' : 'T only';
  const first = t.buyable.indexOf(true);
  const weapons = t.weapons.length
    ? `<ol class="buy-order">${t.weapons.map((w, i) => `<li class="${i === first ? 'first' : t.buyable[i] ? '' : 'no'}">${esc(w)}${t.buyable[i] ? '' : ` <small>(${other}, skipped)</small>`}</li>`).join('')}</ol>`
    : '<p class="hint">No preference: buys a random allowed weapon.</p>';
  const head = foe
    ? `<p>${esc(t.level)} · ${esc(t.model)}</p><p class="md-status">${esc(t.facedOn.length ? `Faced on ${t.facedOn.join(', ')}` : 'Never faced in the career')}</p>`
    : `<p>Tier ${t.cost} · costs ${t.cost} point${t.cost > 1 ? 's' : ''}</p><p class="md-status">${esc(t.selected ? 'Hired' : t.selectable ? 'Can be hired' : t.reason)}</p>`;
  el.innerHTML = `
    <div class="md-head"><img src="img/models/${foe ? 't' : ''}${t.skin}.png" alt=""><div><h4>${esc(t.name)}</h4>${head}</div></div>
    ${bar('Skill', t.skill, STAT_HELP.skill)}
    ${bar('Aggression', t.aggression, STAT_HELP.aggression)}
    ${bar('Teamwork', t.teamwork, STAT_HELP.teamwork)}
    <dl class="md-times">
      <dt>Reaction time</dt><dd>${secs(t.reactionTime)}<small>${STAT_HELP.reaction}</small></dd>
      <dt>Attack delay</dt><dd>${secs(t.attackDelay)}<small>${STAT_HELP.delay}</small></dd>
    </dl>
    <h5>Buys, in this order</h5>
    ${weapons}
    <p class="hint">The first weapon it can afford that ${foe ? 'a terrorist' : 'a Counter-Terrorist'} can buy. Values from the game's BotCampaignProfile.db.</p>`;
}

// ---------------------------------------------------------------------------
// Editor actions
// ---------------------------------------------------------------------------

function toggleMap(id) {
  const m = S.ev.maps.find((x) => x.id === id);
  if (!m || !m.playable || S.draft.allDifficulties) return;
  change((d) => {
    d.beaten = m.beaten ? d.beaten.filter((x) => x !== id) : [...d.beaten, id];
  });
}

function toggleMate(name) {
  const t = S.ev.teammates.find((x) => x.name === name);
  if (!t) return;
  if (t.selected) {
    change((d) => { d.team = d.team.filter((x) => x !== name); });
  } else if (t.selectable) {
    change((d) => { d.team = [...d.team, name]; });
  } else {
    toast(t.reason, true);
  }
}

async function save(overwrite = false) {
  S.draft.name = $('#f-name').value.trim();
  try {
    const r = await api('/api/savestate/save', { savestate: S.draft, original: S.original, overwrite });
    S.original = r.savestate.name;
    S.draft = r.savestate;
    S.dirty = false;
    await reloadState(r.state);
    await evaluate();
    const extra = r.warnings && r.warnings.length ? ' ' + r.warnings.join(' ') : '';
    toast(`Saved. ${gameFileName(r.savestate.name)} is in your Save folder.${extra}`);
  } catch (e) {
    if (e.status === 409 && e.data && e.data.needsOverwrite) {
      if ((await ask({ title: 'Overwrite this profile?', text: e.message, ok: 'Overwrite', danger: true })) !== null) return save(true);
      return;
    }
    showError(e);
  }
}

function uniqueName(base) {
  const names = new Set(S.app.savestates.map((v) => v.savestate.name.toLowerCase()));
  let stem = base.replace(/_\d+$/, '');
  for (let i = 2; i < 100; i++) {
    const n = `${stem}_${i}`.slice(0, 31);
    if (!names.has(n.toLowerCase())) return n;
  }
  return stem;
}

async function duplicate() {
  S.original = null;
  S.draft.name = uniqueName(S.draft.name || 'Savestate');
  $('#f-name').value = S.draft.name;
  S.dirty = true;
  await evaluate();
  renderList();
  $('#f-name').focus();
  $('#f-name').select();
  toast('Copy ready: rename it, change what you need, then save.');
}

async function removeSavestate() {
  const name = S.original;
  const ok = await ask({ title: `Delete ${name}?`, text: `It leaves the bundle, and ${gameFileName(name)} is deleted from your Save folder.`, ok: 'Delete', danger: true });
  if (ok === null) return;
  try {
    await reloadState(await api('/api/savestate/delete', { name }));
    S.dirty = false;
    await openFirstOrNew();
    toast(`${name} deleted.`);
  } catch (e) { showError(e); }
}

async function resetBundle() {
  try {
    const r = await api('/api/bundle/reset', {});
    await reloadState(r.state);
    if (r.errors && r.errors.length) toast(`Restored ${r.restored}, failed: ${r.errors.join('; ')}`, true);
    else toast(`Bundle reset: ${r.restored} savestate${r.restored > 1 ? 's' : ''} restored.`);
  } catch (e) { showError(e); }
}

// ---------------------------------------------------------------------------
// Import and ToD pack
// ---------------------------------------------------------------------------

async function importProfiles() {
  if (S.app.saveDirError) { toast('Set your Save folder in Settings first.', true); return; }
  if (!(await confirmDiscard())) return;
  let list;
  try { list = await api('/api/import'); } catch (e) { showError(e); return; }
  const rows = list.map((p) => {
    const meta = p.importable
      ? `${p.difficulty ? DIFF_LABEL[p.difficulty] : 'No career'} · ${p.beaten} won${p.readOnly ? ' · read-only' : ''}`
      : p.reason;
    return `<li><label class="${p.importable ? '' : 'no'}"><input type="checkbox" name="file" value="${esc(p.file)}" ${p.importable ? '' : 'disabled'}>
      <b>${esc(p.name)}</b><small>${esc(meta)}</small></label></li>`;
  }).join('');
  const body = list.length
    ? `<p>Pick the profiles of your Save folder to add to <b>${esc(S.app.active)}</b>. Their files are left as they are.</p><ul class="import-list">${rows}</ul>`
    : '<p>Your Save folder has no profile.</p>';
  const r = await formDialog({
    title: 'Import from the Save folder', body, ok: 'Import',
    onInput: (dlg) => { $('#form-ok').disabled = !$$('input[name=file]:checked', dlg).length; },
    submit: async (dlg) => {
      const files = $$('input[name=file]:checked', dlg).map((x) => x.value);
      return api('/api/import', { files });
    },
  });
  $('#form-ok').disabled = false;
  if (!r) return;
  await reloadState(r.state);
  const notes = r.results.flatMap((x) => (x.error ? [`${x.name || x.file}: ${x.error}`] : x.notices.map((n) => `${x.name}: ${n}`)));
  const first = r.results.find((x) => !x.error);
  if (first) await openSavestate(first.name, true);
  if (notes.length) {
    await ask({ title: `${r.imported} savestate${r.imported === 1 ? '' : 's'} imported`, text: notes.join('\n') });
  } else {
    toast(`${r.imported} savestate${r.imported === 1 ? '' : 's'} imported.`);
  }
}

async function makePack() {
  if (S.app.saveDirError) { toast('Set your Save folder in Settings first.', true); return; }
  if (!(await confirmDiscard())) return;
  const diffOptions = DIFFS.map((d) => `<option value="${d}">${DIFF_LABEL[d]}</option>`).join('');
  const body = `
    <p>Creates a new bundle with 12 savestates, all with an empty team. For each tour n, the tours before it are won
      and its first map is preselected:</p>
    <p><b>ToDn-U</b> is writable: play the whole tour, the game records your wins.<br>
      <b>ToDn-L</b> is read-only: win a map, reload the profile, play it again.</p>
    <div class="field"><label for="pack-pseudo">Pseudo</label><input id="pack-pseudo" maxlength="24" spellcheck="false" autocomplete="off" placeholder="e.g. ValR"></div>
    <div class="field"><label for="pack-diff">Difficulty</label><select id="pack-diff">${diffOptions}</select></div>
    <div class="pack-names" id="pack-names"></div>`;
  const preview = (dlg) => {
    const pseudo = $('#pack-pseudo', dlg).value.trim();
    const bad = pseudo === '' ? 'Type a pseudo.' : checkNameLocally(`${pseudo}_ToD1-U`);
    $('#pack-names', dlg).textContent = bad || [1, 2, 3, 4, 5, 6].flatMap((n) => [`${pseudo}_ToD${n}-U`, `${pseudo}_ToD${n}-L`]).join(' · ');
    $('#pack-names', dlg).classList.toggle('bad', !!bad);
    $('#form-ok').disabled = !!bad;
  };
  let overwrite = false; // set once the user has seen which profiles would be replaced
  const r = await formDialog({
    title: 'Recommended ToD pack', body, ok: 'Create the pack',
    onInput: (dlg) => {
      preview(dlg);
      if (overwrite) { overwrite = false; $('#form-ok').textContent = 'Create the pack'; $('#form-error').hidden = true; }
    },
    submit: async (dlg) => {
      try {
        return await api('/api/pack', { pseudo: $('#pack-pseudo', dlg).value.trim(), difficulty: $('#pack-diff', dlg).value, overwrite });
      } catch (e) {
        if (e.status === 409 && e.data && e.data.needsOverwrite) {
          overwrite = true;
          $('#form-ok').textContent = 'Overwrite and create';
        }
        throw e;
      }
    },
  });
  $('#form-ok').disabled = false;
  if (!r) return;
  await reloadState(r.state);
  await openFirstOrNew();
  const extra = r.warnings && r.warnings.length ? ' ' + r.warnings.join(' ') : '';
  toast(`Bundle ${r.bundle} created: 12 savestates written to your Save folder.${extra}`);
}

// ---------------------------------------------------------------------------
// Bundles
// ---------------------------------------------------------------------------

async function bundleAction(body, after) {
  try {
    await reloadState(await api('/api/bundles', body));
    if (after) await after();
  } catch (e) { showError(e); }
}

async function selectBundle(name) {
  if (!(await confirmDiscard())) { renderTop(); return; }
  await bundleAction({ action: 'select', name }, () => openFirstOrNew());
}

async function createBundle() {
  if (!(await confirmDiscard())) return;
  const name = await ask({ title: 'New bundle', text: 'Letters, digits, spaces and _-.() only.', input: '', ok: 'Create' });
  if (name) await bundleAction({ action: 'create', name }, () => newSavestate(true));
}

async function renameBundle() {
  const name = await ask({ title: 'Rename bundle', input: S.app.active, ok: 'Rename' });
  if (name && name !== S.app.active) await bundleAction({ action: 'rename', name: S.app.active, newName: name });
}

async function deleteBundle() {
  const n = S.app.savestates.length;
  const ok = await ask({
    title: `Delete the bundle ${S.app.active}?`,
    text: n ? `Its ${n} profile${n > 1 ? 's are' : ' is'} also deleted from your Save folder.` : 'It is empty.',
    ok: 'Delete', danger: true,
  });
  if (ok !== null) await bundleAction({ action: 'delete', name: S.app.active }, () => openFirstOrNew());
}

// ---------------------------------------------------------------------------
// Autosplitter
// ---------------------------------------------------------------------------

async function loadProfiles() {
  try {
    S.profiles = await api('/api/profiles');
  } catch {
    S.profiles = [];
  }
  renderWatchProfiles();
}

// The list shows the name written in each profile (the one the game shows);
// the value is its file, which is what gets watched.
function renderWatchProfiles() {
  const sel = $('#watch-profile');
  const st = S.as.status;
  const current = st && st.enabled && st.file ? st.file : (S.app && S.app.settings.watchFile) || '';
  const list = S.profiles.slice();
  if (current && !list.some((p) => p.file === current)) list.unshift({ file: current, name: current, missing: true });
  sel.innerHTML = list.length
    ? list.map((p) => {
      const label = p.missing ? `${p.name} (not in the Save folder)` : `${p.name} (${p.beaten} won${p.readOnly ? ', read-only' : ''})`;
      return `<option value="${esc(p.file)}" ${p.file === current ? 'selected' : ''}>${esc(label)}</option>`;
    }).join('')
    : '<option value="">No profile in the Save folder</option>';
}

async function pollAutosplit() {
  try {
    const st = await api(`/api/autosplit?since=${S.as.since}`);
    if (st.events && st.events.length) {
      // The watched profile changed on disk: refresh the savestate states.
      if (st.events.some((e) => ['save', 'split', 'reset'].includes(e.kind))) {
        refreshState();
        if (!$('#tab-autosplit').hidden) loadProfiles(); // keep the "N won" of the list current
      }
      S.as.events.push(...st.events);
      S.as.events = S.as.events.slice(-300);
      S.as.since = st.events[st.events.length - 1].id;
    }
    S.as.status = st;
    renderAutosplit();
  } catch { /* the helper may be restarting */ }
}

function renderAutosplit() {
  const st = S.as.status;
  if (!st) return;
  const pill = $('#watch-pill');
  const phase = st.connected ? (st.timerPhase || 'connected') : 'offline';
  pill.className = `watch-pill ${st.enabled ? (st.connected ? 'on' : 'problem') : ''}`;
  pill.innerHTML = st.enabled
    ? `<span class="dot"></span>Watching ${esc(st.profile)} · ${esc(st.timerName)} ${esc(phase)}`
    : '<span class="dot"></span>Autosplit off';

  const ls = $('#ls-status');
  ls.textContent = st.connected ? `Connected to ${st.timerName}. Timer: ${st.timerPhase}.` : (st.linkError || 'Not connected.');
  ls.className = `status-line ${st.connected ? 'good' : 'bad'}`;

  const btn = $('#watch-toggle');
  btn.textContent = st.enabled ? 'Stop watching' : 'Start watching';
  btn.classList.toggle('primary', !st.enabled);

  $('#as-live').innerHTML = `
    <div class="stat"><b>${st.enabled ? esc(st.profile) : '-'}</b><span>Watched profile</span></div>
    <div class="stat"><b>${st.enabled ? st.beaten : '-'}</b><span>Maps won in it</span></div>
    <div class="stat"><b>${st.splits}</b><span>Splits sent</span></div>
    <div class="stat"><b>${esc(st.connected ? st.timerPhase : 'offline')}</b><span>${esc(st.timerName)} timer</span></div>`;

  $('#as-log').innerHTML = S.as.events.slice().reverse().map((e) => {
    const t = new Date(e.time);
    const time = t.toLocaleTimeString([], { hour12: false }) + '.' + String(t.getMilliseconds()).padStart(3, '0');
    return `<li class="${esc(e.kind)}"><time>${esc(time)}</time><span class="kind">${esc(e.kind)}</span><span>${esc(e.text)}</span></li>`;
  }).join('') || '<li><span></span><span></span><span class="hint">Nothing yet.</span></li>';
}

async function toggleWatch() {
  const st = S.as.status;
  try {
    if (st && st.enabled) {
      await api('/api/autosplit', { enabled: false, file: '' });
    } else {
      const file = $('#watch-profile').value;
      if (!file) { toast('Pick a profile to watch first.', true); return; }
      await api('/api/autosplit', { enabled: true, file });
    }
    await pollAutosplit();
  } catch (e) { showError(e); }
}

// ---------------------------------------------------------------------------
// Tabs and wiring
// ---------------------------------------------------------------------------

function showTab(name) {
  if (!['savestates', 'autosplit', 'settings'].includes(name)) return;
  $$('.tabs button').forEach((b) => b.setAttribute('aria-selected', String(b.dataset.tab === name)));
  $$('.tab').forEach((t) => { t.hidden = t.id !== `tab-${name}`; });
  if (name === 'autosplit') loadProfiles();
  if (name === 'savestates') refreshState();
  try { localStorage.setItem('todh-tab', name); } catch { /* storage blocked */ }
}

function bind() {
  $$('.tabs button').forEach((b) => b.addEventListener('click', () => showTab(b.dataset.tab)));
  document.addEventListener('click', (e) => {
    const go = e.target.closest('[data-goto]');
    if (go) showTab(go.dataset.goto);
  });

  $('#savestate-list').addEventListener('click', (e) => {
    const b = e.target.closest('[data-open]');
    if (b) openSavestate(b.dataset.open).catch(showError);
  });
  $('#new-savestate').addEventListener('click', () => newSavestate().catch(showError));
  $('#import-btn').addEventListener('click', () => importProfiles().catch(showError));
  $('#pack-btn').addEventListener('click', () => makePack().catch(showError));

  const name = $('#f-name');
  name.addEventListener('input', () => {
    S.draft.name = name.value;
    S.dirty = true;
    renderFileName();
    $('#save-btn').textContent = S.original === null ? 'Save savestate' : 'Save changes';
    $('#save-btn').disabled = !nameOK() || !!S.app.saveDirError;
  });
  name.addEventListener('change', () => evaluate().catch(showError));
  name.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); save(); } });

  $('#f-diff').addEventListener('click', (e) => {
    const b = e.target.closest('[data-diff]');
    if (b && b.dataset.diff !== S.draft.difficulty) change((d) => { d.difficulty = b.dataset.diff; });
  });
  $('#f-ro').addEventListener('change', (e) => change((d) => { d.readOnly = e.target.checked; }));
  $('#f-all').addEventListener('change', (e) => change((d) => { d.allDifficulties = e.target.checked; }));
  $('#f-cheat').addEventListener('change', (e) => change((d) => { d.cheat = e.target.checked; }));

  $('#tours').addEventListener('click', (e) => {
    const start = e.target.closest('[data-start]');
    if (start && !start.disabled) { change((d) => { d.current = start.dataset.start; }); return; }
    const tile = e.target.closest('[data-map]');
    if (tile) toggleMap(tile.dataset.map);
  });
  const mateClick = (e) => {
    const b = e.target.closest('[data-mate]');
    if (b) {
      S.detail = b.dataset.mate;
      toggleMate(b.dataset.mate);
    }
  };
  $('#pool').addEventListener('click', mateClick); // terrorist cards have no data-mate: nothing to hire
  $('#side').addEventListener('click', (e) => {
    const b = e.target.closest('[data-side]');
    if (!b || b.dataset.side === S.side) return;
    S.side = b.dataset.side;
    S.detail = null;
    renderTeam();
  });
  $('#hired').addEventListener('click', mateClick);
  const showDetail = (e) => {
    const b = e.target.closest('[data-mate], [data-foe]');
    if (!b) return;
    const name = b.dataset.mate || b.dataset.foe;
    const side = b.dataset.foe ? 't' : 'ct';
    if (side !== S.side || name === S.detail) return;
    S.detail = name;
    $$('#pool .mate').forEach((m) => m.classList.toggle('focused', (m.dataset.mate || m.dataset.foe) === S.detail));
    renderMateDetail();
  };
  for (const id of ['#pool', '#hired']) {
    $(id).addEventListener('mouseover', showDetail);
    $(id).addEventListener('focusin', showDetail);
  }

  $('#save-btn').addEventListener('click', () => save());
  $('#dup-btn').addEventListener('click', () => duplicate().catch(showError));
  $('#del-btn').addEventListener('click', () => removeSavestate());
  $('#reset-btn').addEventListener('click', () => resetBundle());

  $('#bundle-select').addEventListener('change', (e) => selectBundle(e.target.value));
  $('#bundle-new').addEventListener('click', () => createBundle());
  $('#bundle-rename').addEventListener('click', () => renameBundle());
  $('#bundle-delete').addEventListener('click', () => deleteBundle());

  $('#watch-pill').addEventListener('click', () => showTab('autosplit'));
  $('#watch-toggle').addEventListener('click', () => toggleWatch());
  $('#watch-refresh').addEventListener('click', () => loadProfiles());
  $('#ls-save').addEventListener('click', async () => {
    try {
      await reloadState(await api('/api/settings', { liveSplitAddr: $('#ls-addr').value }));
      toast('LiveSplit address saved.');
      pollAutosplit();
    } catch (e) { showError(e); }
  });
  $('#timer-kind').addEventListener('click', async (e) => {
    const b = e.target.closest('[data-timer]');
    if (!b || b.dataset.timer === S.app.settings.timerKind) return;
    try {
      await reloadState(await api('/api/settings', { timerKind: b.dataset.timer }));
      pollAutosplit();
    } catch (err) { showError(err); }
  });
  $('#lso-copy').addEventListener('click', async () => {
    const url = $('#lso-url').textContent;
    try {
      await navigator.clipboard.writeText(url);
      toast('Address copied.');
    } catch {
      const r = document.createRange();
      r.selectNodeContents($('#lso-url'));
      const sel = window.getSelection();
      sel.removeAllRanges();
      sel.addRange(r);
      toast('Press Ctrl+C to copy the selected address.');
    }
  });
  $('#save-dir-browse').addEventListener('click', async () => {
    const btn = $('#save-dir-browse');
    btn.disabled = true;
    btn.textContent = 'Choose in the window…';
    try {
      const r = await api('/api/browse', {});
      if (r.message) toast(r.message, true);
      if (!r.cancelled) {
        $('#save-dir').value = r.path;
        await saveDir();
      }
    } catch (err) {
      showError(err);
    } finally {
      btn.disabled = false;
      btn.textContent = 'Browse…';
    }
  });
  const saveDir = async () => {
    try {
      await reloadState(await api('/api/settings', { saveDir: $('#save-dir').value }));
      await evaluate();
      toast('Save folder set.');
    } catch (e) { showError(e); }
  };
  $('#save-dir-btn').addEventListener('click', saveDir);
  $('#save-dir').addEventListener('keydown', (e) => { if (e.key === 'Enter') saveDir(); });

  window.addEventListener('beforeunload', (e) => { if (S.dirty) { e.preventDefault(); e.returnValue = ''; } });
}

async function boot() {
  bind();
  try {
    S.terrorists = await api('/api/terrorists');
  } catch { S.terrorists = []; }
  try {
    await reloadState();
    await openFirstOrNew();
    let tab = 'savestates';
    try { tab = localStorage.getItem('todh-tab') || tab; } catch { /* storage blocked */ }
    showTab(S.app.saveDirError && !S.app.settings.saveDir ? 'settings' : tab);
  } catch (e) {
    showError(e);
  }
  pollAutosplit();
  setInterval(pollAutosplit, 800);
  setInterval(() => { if (document.visibilityState === 'visible') refreshState(); }, 3000);
  window.addEventListener('focus', refreshState);
}

boot();

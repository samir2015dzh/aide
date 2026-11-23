// The bots' faces on Android (工单 #83): one hidden page runs the Grok Bot engine ported from BotArk
// (packages/ui/src/grok/grokMotionEngine.js, copied beside this at build time; its globals are G) for every face on
// screen, as createMotionEngine runs one on the desktop, without a DOM to draw in: each frame it hands back what to
// draw, flattened to a few kinds of shapes, for Compose to paint (ui/GrokFace.kt).
//
// aide.set(id, o): o is { shape, seed, state, size, playing, emphasized, follow, still }; aide.drop, hold, gaze, poke.
// aide.tick(dt): advances every face by dt seconds; returns { id: frame } for those that moved, frame as flat() says.
(function () {
  var marks = new Map();
  var appearance = G.createAppearance("ink");
  var FILL = 0.92;

  // The engine's own seed hash (createMotionEngine's u_), for the state machine's clip order.
  function seedOf(s) {
    var t = 2166136261;
    for (var n = 0; n < s.length; n++) t = Math.imul(t ^ s.charCodeAt(n), 16777619);
    return t >>> 0;
  }

  function make(o) {
    var rig = G.createRig({ body: G.mapShapeToBody(o.shape) });
    var ctl = G.createStateController(rig, o.state, { seed: seedOf(o.seed), isCompact: o.size < 48, isResting: !o.playing || o.still });
    var m = { rig: rig, ctl: ctl, o: o, dirty: true };
    rig.setGazeAlways(!!o.follow);
    ctl.setEmphasis(!!o.emphasized);
    settle(m);
    return m;
  }

  // A still face (the system's reduce motion, as the engine's isStatic): its pose a few seconds in, held.
  function settle(m) {
    if (!m.o.still) return;
    m.rig.pause();
    m.rig.seek(0);
    for (var i = 0; i < 60; i++) m.rig.advance(0.1);
  }

  function update(m, o) {
    var was = m.o;
    m.o = o;
    if (was.shape !== o.shape) m.rig.setBody(G.mapShapeToBody(o.shape), { isInstant: !!o.still });
    if (was.state !== o.state) m.ctl.setState(o.state);
    if (was.size !== o.size) m.ctl.setCompact(o.size < 48);
    if (was.emphasized !== o.emphasized) m.ctl.setEmphasis(!!o.emphasized);
    if (was.follow !== o.follow) m.rig.setGazeAlways(!!o.follow);
    if ((was.playing && !was.still) !== (o.playing && !o.still)) m.ctl.setResting(!o.playing || o.still);
    settle(m);
    m.dirty = true;
  }

  function matrix(t) {
    var m = [1, 0, 0, 1, 0, 0];
    if (!t) return m;
    var re = /(\w+)\(([^)]*)\)/g, x;
    while ((x = re.exec(t))) {
      var a = x[2].trim().split(/[\s,]+/).map(Number), n;
      switch (x[1]) {
        case "matrix": n = a; break;
        case "translate": n = [1, 0, 0, 1, a[0], a[1] || 0]; break;
        case "scale": n = [a[0], 0, 0, a.length > 1 ? a[1] : a[0], 0, 0]; break;
        case "rotate": {
          var r = (a[0] * Math.PI) / 180, c = Math.cos(r), s = Math.sin(r), cx = a[1] || 0, cy = a[2] || 0;
          n = [c, s, -s, c, cx - c * cx + s * cy, cy - s * cx - c * cy];
          break;
        }
        default: continue;
      }
      m = mul(m, n);
    }
    return m;
  }

  function mul(a, b) {
    return [a[0] * b[0] + a[2] * b[1], a[1] * b[0] + a[3] * b[1], a[0] * b[2] + a[2] * b[3], a[1] * b[2] + a[3] * b[3], a[0] * b[4] + a[2] * b[5] + a[4], a[1] * b[4] + a[3] * b[5] + a[5]];
  }

  // A frame, flat: { v: viewBox size, ops: [...] }, each op one shape in the ink —
  // { k: "p", d, m, o, h?, e? } a filled path (h: the holes its mask cuts, the eyes; e: even-odd),
  // { k: "s", d, m, o, w } a stroked path, { k: "c", x, y, r, m, o, w?, a? } a circle (w: stroked, a: [dash, gap]);
  // m the matrix [a b c d e f] it is drawn with, o its opacity. Grey details (#3d3d3d, #525252) carry f.
  function flat(svg) {
    var ids = {};
    (function index(n) {
      if (n.attrs.id) ids[n.attrs.id] = n;
      n.children.forEach(index);
    })(svg);
    var holes = function (mask) {
      var id = /url\(#([^)]+)\)/.exec(mask || "");
      var node = id && ids[id[1]];
      return node ? node.children.filter(function (c) { return c.tag === "path"; }).map(function (c) { return c.attrs.d; }).join("") : undefined;
    };
    var ops = [];
    (function walk(n, m, o) {
      var a = n.attrs;
      var op = o * (a.opacity === undefined ? 1 : Number(a.opacity));
      switch (n.tag) {
        case "svg":
          n.children.forEach(function (c) { walk(c, m, o); });
          return;
        case "g":
          var gm = mul(m, matrix(a.transform));
          n.children.forEach(function (c) { walk(c, gm, op); });
          return;
        case "use": {
          var ref = ids[String(a.href).slice(1)];
          if (!ref) return;
          var d = ref.children.map(function (c) { return c.attrs.d; }).join("");
          ops.push({ k: "p", d: d, m: mul(m, matrix(a.transform)), o: op, h: holes(a.mask) });
          return;
        }
        case "path": {
          var pm = mul(m, matrix(a.transform));
          if (a.stroke) ops.push({ k: "s", d: a.d, m: pm, o: op, w: Number(a["stroke-width"]) });
          else if (a.fill !== "none") ops.push({ k: "p", d: a.d, m: pm, o: op, h: holes(a.mask), e: a["fill-rule"] === "evenodd" || undefined, f: a.fill === "ink" ? undefined : a.fill });
          return;
        }
        case "circle": {
          var c = { k: "c", x: Number(a.cx), y: Number(a.cy), r: Number(a.r), m: mul(m, matrix(a.transform)), o: op };
          if (a.stroke) c.w = Number(a["stroke-width"]);
          if (a["stroke-dasharray"]) c.a = String(a["stroke-dasharray"]).split(/\s+/).map(Number);
          ops.push(c);
          return;
        }
        default:
          return; // defs, masks: drawn where they are used
      }
    })(svg, [1, 0, 0, 1, 0, 0], 1);
    return { v: Number(String(svg.attrs.viewBox).split(" ")[2]), ops: ops };
  }

  function draw(id, m) {
    var res = Math.round(m.o.size * G.computeCanvasScale(FILL) * 100) / 100;
    return flat(G.renderFrameToTree(m.rig.frame(), appearance, res, id, { displaySize: m.o.size, extent: "100%" }));
  }

  globalThis.aide = {
    set: function (id, o) {
      var m = marks.get(id);
      if (m) update(m, o);
      else marks.set(id, make(o));
    },
    drop: function (id) {
      marks.delete(id);
    },
    // Held: its frame stays as it is (a list scrolling, 工单 #55), and it goes on from there when let go.
    hold: function (id, held) {
      var m = marks.get(id);
      if (m) m.held = held;
    },
    gaze: function (id, x, y) {
      var m = marks.get(id);
      if (!m || m.o.still) return;
      m.rig.setGaze(x === null ? null : [x, y]);
      m.dirty = true;
    },
    poke: function (id, kind) {
      var m = marks.get(id);
      if (!m || m.o.still) return;
      m.rig.poke(kind);
      m.dirty = true;
    },
    // As createMotionEngine's loop: a playing face advances its state and its rig; one that is not still advances
    // its rig until it settles (a poke, a turn of its head), then is left alone.
    tick: function (dt) {
      var out = {};
      marks.forEach(function (m, id) {
        var playing = m.o.playing && !m.o.still;
        if (m.held || (!playing && !m.dirty && m.rig.isSettled)) return;
        if (!m.o.still) {
          if (playing) m.ctl.advance(dt);
          m.rig.advance(dt);
        }
        m.dirty = false;
        out[id] = draw(id, m);
      });
      return out;
    },
  };
})();

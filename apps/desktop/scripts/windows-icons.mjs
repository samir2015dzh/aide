// The Windows app's icon: build/Aide.ico, drawn by hand for every size it
// holds (16-24 px pixel by pixel, so the mark stays a ring with two dots;
// the handover's design/aide/icons/final-g3, whose
// tools/ draw it again). readPNG is the splash's (windows-splash.mjs), which
// draws from the macOS icon's mark. No image library: PNGs are read with zlib.
import fs from "node:fs";
import path from "node:path";
import zlib from "node:zlib";

const desktop = path.resolve(import.meta.dirname, "..");

/** Decodes an 8-bit RGBA, non-interlaced PNG. */
export function readPNG(buf) {
  let pos = 8;
  let width = 0, height = 0;
  const idat = [];
  while (pos < buf.length) {
    const len = buf.readUInt32BE(pos);
    const type = buf.toString("latin1", pos + 4, pos + 8);
    const data = buf.subarray(pos + 8, pos + 8 + len);
    if (type === "IHDR") {
      width = data.readUInt32BE(0);
      height = data.readUInt32BE(4);
      if (data[8] !== 8 || data[9] !== 6 || data[12] !== 0) throw new Error("only 8-bit RGBA, non-interlaced PNGs");
    } else if (type === "IDAT") idat.push(data);
    pos += 12 + len;
  }
  const raw = zlib.inflateSync(Buffer.concat(idat));
  const stride = width * 4;
  const px = Buffer.alloc(stride * height);
  for (let y = 0; y < height; y++) {
    const f = raw[y * (stride + 1)];
    const row = raw.subarray(y * (stride + 1) + 1, (y + 1) * (stride + 1));
    for (let x = 0; x < stride; x++) {
      const a = x >= 4 ? px[y * stride + x - 4] : 0;
      const b = y > 0 ? px[(y - 1) * stride + x] : 0;
      const c = x >= 4 && y > 0 ? px[(y - 1) * stride + x - 4] : 0;
      const p = a + b - c;
      const pa = Math.abs(p - a), pb = Math.abs(p - b), pc = Math.abs(p - c);
      const pred = [0, a, b, (a + b) >> 1, pa <= pb && pa <= pc ? a : pb <= pc ? b : c][f];
      px[y * stride + x] = (row[x] + pred) & 255;
    }
  }
  return { width, height, px };
}

/** Writes the program's icon (Aide.exe's, its shortcuts' and Setup.exe's) to icoPath. */
export function windowsIcons(icoPath) {
  fs.copyFileSync(path.join(desktop, "build/Aide.ico"), icoPath);
}

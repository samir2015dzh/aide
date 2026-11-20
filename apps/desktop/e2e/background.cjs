// Loaded before the app in every test launch (electron -r): windows open
// behind everything else without taking focus, so the tests don't take over
// the Mac they run on. Playwright drives pages over CDP, which needs neither.
const { app, BrowserWindow } = require("electron");

BrowserWindow.prototype.show = function () {
  this.showInactive();
  this.blur(); // On macOS this orders the window to the back.
};
BrowserWindow.prototype.focus = function () {};
app.focus = () => {};
// Linux, where the suite can run too, takes the Mac's platform code (src/main/platform), whose traffic lights it
// has none of: without this, placing them throws in the main process, and Electron's error box blocks it.
if (!BrowserWindow.prototype.setWindowButtonPosition) BrowserWindow.prototype.setWindowButtonPosition = () => {};

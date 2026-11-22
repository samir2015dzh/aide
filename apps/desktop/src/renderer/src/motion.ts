// The app's motion (工单 #47): what moves says something is happening, and
// nothing moves for nobody. While the window is hidden or minimized every
// animation stops — CSS ones by the class below (motion.css), those the
// Web Animations API runs (the map's comets) paused here and resumed when it
// shows again, so a resident app spends nothing on motion no one sees.

export function stillWhileHidden(doc: Document = document): () => void {
  const paused = new Set<Animation>();
  const apply = () => {
    const hidden = doc.hidden;
    doc.documentElement.classList.toggle("window-hidden", hidden);
    if (hidden) {
      for (const a of doc.getAnimations()) {
        if (a.playState === "running") {
          a.pause();
          paused.add(a);
        }
      }
      return;
    }
    // Only those paused here: one that ended meanwhile is not started again.
    for (const a of paused) if (a.playState === "paused") a.play();
    paused.clear();
  };
  doc.addEventListener("visibilitychange", apply);
  apply();
  return () => doc.removeEventListener("visibilitychange", apply);
}

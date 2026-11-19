//! Assigns sequence numbers and delivers events in that order.

use her_llm::now_ms;
use her_protocol::Event;
use her_protocol::EventMsg;
use parking_lot::Mutex;
use tokio::sync::mpsc;

/// Events travel boxed: the channel keeps its blocks of slots for the life of the session, and
/// a slot for a box is 8 bytes instead of the size of the largest event.
pub(crate) struct Emitter {
    inner: Mutex<(u64, mpsc::UnboundedSender<Box<Event>>)>,
}

impl Emitter {
    pub(crate) fn new(tx: mpsc::UnboundedSender<Box<Event>>) -> Self {
        Self {
            inner: Mutex::new((0, tx)),
        }
    }

    /// Sequence assignment and delivery happen under one lock, so `seq` order is delivery order.
    pub(crate) fn emit(&self, msg: EventMsg) {
        self.emit_at(msg, now_ms());
    }

    /// As `emit`, stamped `ts`: the time of what the event tells, when the log recorded it
    /// already (a task's end), so that the event and the log say the same moment.
    pub(crate) fn emit_at(&self, msg: EventMsg, ts: her_llm::Timestamp) {
        let mut inner = self.inner.lock();
        inner.0 += 1;
        let event = Box::new(Event { seq: inner.0, ts, msg });
        let _ = inner.1.send(event);
    }
}

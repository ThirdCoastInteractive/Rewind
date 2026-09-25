// Serialize SDP negotiation and hold trickled ICE until the first offer is set.
export function createSignalingQueue({ onOffer, onCandidate, onMeta, onError = () => {} }) {
  let remoteDescriptionReady = false;
  const pendingCandidates = [];
  let chain = Promise.resolve();

  async function process(raw) {
    let message;
    try {
      message = typeof raw === 'string' ? JSON.parse(raw) : raw;
    } catch {
      return;
    }
    if (!message || typeof message.event !== 'string') return;

    if (message.event === 'candidate' && !remoteDescriptionReady) {
      pendingCandidates.push(message);
      return;
    }
    if (message.event === 'offer') {
      await onOffer(message);
      remoteDescriptionReady = true;
      while (pendingCandidates.length > 0) {
        await onCandidate(pendingCandidates.shift());
      }
      return;
    }
    if (message.event === 'candidate') {
      await onCandidate(message);
      return;
    }
    if (message.event === 'meta') await onMeta?.(message);
  }

  return {
    push(raw) {
      chain = chain.then(() => process(raw)).catch(onError);
      return chain;
    },
    whenIdle() {
      return chain;
    },
  };
}

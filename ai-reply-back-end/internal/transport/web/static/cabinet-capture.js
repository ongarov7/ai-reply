/*
 * AudioWorklet for the cabinet's dictation (cabinet.js). It only copies the
 * microphone's samples to the page, mixed down to mono, in small batches;
 * the page downsamples them to 16 kHz and encodes the WAV. Nothing is kept here.
 */
class AIReplyCapture extends AudioWorkletProcessor {
  constructor() {
    super();
    this.batch = new Float32Array(1024);
    this.length = 0;
  }

  process(inputs) {
    const input = inputs[0];
    if (input && input.length > 0) {
      const frames = input[0].length;
      for (let i = 0; i < frames; i++) {
        let sum = 0;
        for (let channel = 0; channel < input.length; channel++) sum += input[channel][i];
        this.batch[this.length++] = sum / input.length;
        if (this.length === this.batch.length) {
          this.port.postMessage(this.batch.slice(0));
          this.length = 0;
        }
      }
    }
    return true;
  }
}

registerProcessor('aireply-capture', AIReplyCapture);

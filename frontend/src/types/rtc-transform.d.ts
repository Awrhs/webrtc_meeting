interface RTCEncodedAudioFrame {
  data: ArrayBuffer
  getMetadata(): {
    synchronizationSource?: number
    payloadType?: number
    contributingSources?: number[]
  }
}

interface RTCEncodedVideoFrame {
  data: ArrayBuffer
  type?: 'key' | 'delta'
  getMetadata(): {
    synchronizationSource?: number
    payloadType?: number
    frameId?: number | null
    dependencies?: number[]
    width?: number | null
    height?: number | null
    spatialIndex?: number | null
    temporalIndex?: number | null
    contributingSources?: number[]
  }
}

type RTCEncodedFrame = RTCEncodedAudioFrame | RTCEncodedVideoFrame

interface RTCRtpScriptTransformerOptions {
  direction: 'send' | 'recv'
  trackId: string
  kind: string
}

interface RTCRtpScriptTransformer {
  readable: ReadableStream<RTCEncodedFrame>
  writable: WritableStream<RTCEncodedFrame>
  options: RTCRtpScriptTransformerOptions
}

interface RTCRtpTransformEvent extends Event {
  transformer: RTCRtpScriptTransformer
}

declare class RTCRtpScriptTransform {
  constructor(worker: Worker, options?: RTCRtpScriptTransformerOptions, transfer?: Transferable[])
}

interface RTCRtpSender {
  transform?: RTCRtpScriptTransform | null
}

interface RTCRtpReceiver {
  transform?: RTCRtpScriptTransform | null
}

interface RTCConfiguration {
  encodedInsertableStreams?: boolean
}

declare let onrtctransform: ((event: RTCRtpTransformEvent) => void) | null

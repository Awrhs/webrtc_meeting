import { Room } from '@/types'

const API_HOST = import.meta.env.VITE_API_HOST || 'http://localhost:8080'
const SFU_HOST = import.meta.env.VITE_SFU_HOST || 'http://localhost:8081'

export const createPeerConnection = async (
  roomId: number,
  userId: number,
  onTrack: (track: MediaStreamTrack, stream: MediaStream) => void
): Promise<RTCPeerConnection> => {
  const configuration: RTCConfiguration = {
    iceServers: [
      { urls: 'stun:stun.l.google.com:19302' },
    ],
  }

  const pc = new RTCPeerConnection(configuration)

  pc.onicecandidate = (event) => {
    if (event.candidate) {
    }
  }

  pc.ontrack = (event) => {
    event.streams.forEach((stream) => {
      stream.getTracks().forEach((track) => {
        onTrack(track, stream)
      })
    })
  }

  return pc
}

export const connectToSFU = async (
  roomId: number,
  userId: number,
  localStream: MediaStream
): Promise<RTCPeerConnection> => {
  const pc = await createPeerConnection(roomId, userId, () => {})

  localStream.getTracks().forEach((track) => {
    pc.addTrack(track, localStream)
  })

  const offer = await pc.createOffer()
  await pc.setLocalDescription(offer)

  const response = await fetch(`${SFU_HOST}/offer`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      room_id: roomId,
      user_id: userId,
      sdp: offer.sdp,
    }),
  })

  const answer = await response.json()
  await pc.setRemoteDescription({
    type: 'answer',
    sdp: answer.sdp,
  })

  return pc
}

export const getMediaStream = async (video: boolean, audio: boolean): Promise<MediaStream> => {
  return navigator.mediaDevices.getUserMedia({
    video: video ? {
      width: { ideal: 1920 },
      height: { ideal: 1080 },
    } : false,
    audio: audio ? {
      echoCancellation: true,
      noiseSuppression: true,
      autoGainControl: true,
    } : false,
  })
}

export const enumerateDevices = async (): Promise<{
  videoinputs: MediaDeviceInfo[]
  audioinputs: MediaDeviceInfo[]
  audiooutputs: MediaDeviceInfo[]
}> => {
  const devices = await navigator.mediaDevices.enumerateDevices()
  return {
    videoinputs: devices.filter(d => d.kind === 'videoinput'),
    audioinputs: devices.filter(d => d.kind === 'audioinput'),
    audiooutputs: devices.filter(d => d.kind === 'audiooutput'),
  }
}

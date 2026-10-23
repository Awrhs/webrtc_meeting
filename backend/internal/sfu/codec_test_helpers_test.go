package sfu

import "github.com/pion/webrtc/v3"

// 测试辅助：构造录制测试用的编码参数。

func webrtcCodecVP8() webrtc.RTPCodecParameters {
	return webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:    "video/VP8",
			ClockRate:   90000,
			SDPFmtpLine: "",
		},
		PayloadType: 96,
	}
}

func webrtcCodecOpus() webrtc.RTPCodecParameters {
	return webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:    "audio/opus",
			ClockRate:   48000,
			Channels:    2,
			SDPFmtpLine: "minptime=10;useinbandfec=1",
		},
		PayloadType: 111,
	}
}

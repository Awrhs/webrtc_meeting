import { useCallback, useEffect, useState } from 'react'
import { Alert, Button, List, Modal, Space, Tag, message } from 'antd'
import { DownloadOutlined, ReloadOutlined } from '@ant-design/icons'
import { api } from '@/utils/api'

interface RecordingEntry {
  dir: string
  started_at?: string
  files: string[]
}

interface RecordingsModalProps {
  roomId: number
  open: boolean
  onClose: () => void
  // 本会议是否启用了媒体端到端加密：开启时服务端录制的是密文，要提醒用户
  mediaE2EEEnabled: boolean
}

// 会议录制（房主/管理员）：录制由 SFU 在服务端完成，经 API 做房间权限校验后列出与下载。
export default function RecordingsModal({ roomId, open, onClose, mediaE2EEEnabled }: RecordingsModalProps) {
  const [loading, setLoading] = useState(false)
  const [enabled, setEnabled] = useState(true)
  const [recordings, setRecordings] = useState<RecordingEntry[]>([])
  const [downloading, setDownloading] = useState<string | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const response = await api.get(`/api/v1/rooms/${roomId}/recordings`)
      setEnabled(Boolean(response.data?.enabled))
      setRecordings(response.data?.recordings || [])
    } catch (error: any) {
      message.error(error?.response?.data?.error || '获取录制列表失败')
    } finally {
      setLoading(false)
    }
  }, [roomId])

  useEffect(() => {
    if (open) void load()
  }, [open, load])

  // 下载需要带认证头，<a href> 做不到，先拉取为 blob 再触发保存
  const download = async (dir: string, file: string) => {
    const key = `${dir}/${file}`
    setDownloading(key)
    try {
      const response = await api.get(
        `/api/v1/rooms/${roomId}/recordings/${encodeURIComponent(dir)}/${encodeURIComponent(file)}`,
        { responseType: 'blob', timeout: 0 }
      )
      const url = URL.createObjectURL(response.data as Blob)
      const a = document.createElement('a')
      a.href = url
      a.download = file
      a.click()
      setTimeout(() => URL.revokeObjectURL(url), 0)
    } catch (error: any) {
      message.error(error?.response?.data?.error || '下载失败')
    } finally {
      setDownloading(null)
    }
  }

  return (
    <Modal
      title="会议录制"
      open={open}
      onCancel={onClose}
      width={640}
      footer={[
        <Button key="refresh" icon={<ReloadOutlined />} onClick={() => void load()} loading={loading}>
          刷新
        </Button>,
        <Button key="close" type="primary" onClick={onClose}>
          关闭
        </Button>,
      ]}
    >
      {!enabled && (
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 12 }}
          message="本服务器未开启会议录制"
          description="管理员需在 SFU 服务上设置 SFU_RECORDING_DIR 后，录制才会开始。"
        />
      )}
      {mediaE2EEEnabled && (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 12 }}
          message="本会议启用了媒体端到端加密"
          description="服务端只能录到加密后的音视频数据，录制文件无法直接回放。"
        />
      )}
      <List
        loading={loading}
        dataSource={recordings}
        locale={{ emptyText: enabled ? '暂无录制' : '录制未开启' }}
        renderItem={(entry) => (
          <List.Item>
            <List.Item.Meta
              title={
                <Space>
                  <span>{entry.started_at ? new Date(entry.started_at).toLocaleString() : entry.dir}</span>
                  <Tag>{entry.files.length} 个文件</Tag>
                </Space>
              }
              description={
                <Space wrap>
                  {entry.files.map(file => (
                    <Button
                      key={file}
                      size="small"
                      icon={<DownloadOutlined />}
                      loading={downloading === `${entry.dir}/${file}`}
                      onClick={() => void download(entry.dir, file)}
                    >
                      {file}
                    </Button>
                  ))}
                </Space>
              }
            />
          </List.Item>
        )}
      />
    </Modal>
  )
}

import { describe, expect, it } from 'vitest'
import { GroupE2EEManager } from './encryption'

function check(mgr: GroupE2EEManager, senderId: number, epoch: number, n: number): boolean {
  return (mgr as any).checkAndUpdateReplay(senderId, epoch, n)
}

describe('GroupE2EEManager replay window', () => {
  it('accepts first occurrence and rejects exact duplicate', () => {
    const mgr = new GroupE2EEManager()
    expect(check(mgr, 1, 0, 5)).toBe(true)
    expect(check(mgr, 1, 0, 5)).toBe(false)
  })

  it('accepts increasing message numbers', () => {
    const mgr = new GroupE2EEManager()
    expect(check(mgr, 1, 0, 1)).toBe(true)
    expect(check(mgr, 1, 0, 2)).toBe(true)
    expect(check(mgr, 1, 0, 3)).toBe(true)
  })

  it('accepts out-of-order message within window', () => {
    const mgr = new GroupE2EEManager()
    expect(check(mgr, 1, 0, 10)).toBe(true)
    expect(check(mgr, 1, 0, 8)).toBe(true)
    expect(check(mgr, 1, 0, 8)).toBe(false)
  })

  it('rejects messages outside the window', () => {
    const mgr = new GroupE2EEManager()
    expect(check(mgr, 1, 0, 200)).toBe(true)
    // 200 - 128 = 72 之前的全部拒绝
    expect(check(mgr, 1, 0, 50)).toBe(false)
  })

  it('isolates by senderId and epoch', () => {
    const mgr = new GroupE2EEManager()
    expect(check(mgr, 1, 0, 5)).toBe(true)
    expect(check(mgr, 2, 0, 5)).toBe(true) // 不同 sender
    expect(check(mgr, 1, 1, 5)).toBe(true) // 不同 epoch
    expect(check(mgr, 1, 0, 5)).toBe(false) // 同 sender/epoch
  })

  it('handles large jump by resetting window', () => {
    const mgr = new GroupE2EEManager()
    expect(check(mgr, 1, 0, 5)).toBe(true)
    expect(check(mgr, 1, 0, 1000)).toBe(true) // 跳跃过大，窗口重置
    expect(check(mgr, 1, 0, 1000)).toBe(false) // 但 1000 自己仍在窗口里
  })
})

import { describe, expect, it } from 'vitest'
import { isMediaE2EESupported, MediaE2EEManager } from './mediaE2EE'

describe('MediaE2EEManager', () => {
  it('reports unsupported in jsdom by default', () => {
    expect(isMediaE2EESupported()).toBe(false)
  })

  it('stays disabled when encoded transform is unsupported', () => {
    const manager = new MediaE2EEManager(true)
    expect(manager.isEnabled()).toBe(false)
    expect(manager.getState()).toBe('unsupported')
  })
})

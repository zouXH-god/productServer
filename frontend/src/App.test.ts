import { describe, expect, it, beforeEach } from 'vitest'
import { session } from './api'
import { router } from './router'

describe('authentication routing', () => {
  beforeEach(() => { sessionStorage.clear() })
  it('redirects anonymous users to login', async () => { await router.push('/'); await router.isReady(); expect(router.currentRoute.value.path).toBe('/login') })
  it('allows authenticated users to open projects', async () => { session.token='test'; await router.push('/'); expect(router.currentRoute.value.path).toBe('/') })
})

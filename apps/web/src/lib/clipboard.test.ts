import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { toClipboard } from './clipboard'

/**
 * jsdom has no copy command and no clipboard, so each test stands in a
 * browser: what the command does when it runs is the thing under test, and
 * the one that matters most is the browser that runs it, reports success, and
 * copies nothing — Safari over an empty selection, which is the bug this
 * module exists to be honest about.
 */

/** What the copy event handed a listener, as a browser would. */
type Clipboard = { text: string | null; prevented: boolean }

/** A copy command that behaves like a browser's: it dispatches a copy event
 *  at the selection and lets a listener take over the clipboard. */
function commandThatFires(clip: Clipboard, result = true) {
  return vi.fn(() => {
    const event = new Event('copy', { bubbles: true, cancelable: true }) as Event & {
      clipboardData: { setData: (type: string, data: string) => void }
    }
    event.clipboardData = {
      setData: (type, data) => {
        if (type === 'text/plain') clip.text = data
      },
    }
    const at = document.getSelection()?.anchorNode ?? document.body
    at.dispatchEvent(event)
    clip.prevented = event.defaultPrevented
    return result
  })
}

/** A command that runs, says it did, and never fires: nothing was selected as
 *  far as the browser was concerned, so nothing was copied. */
const commandThatLies = () => vi.fn(() => true)

function setCommand(fn: unknown) {
  Object.defineProperty(document, 'execCommand', { value: fn, configurable: true, writable: true })
}

function setModern(writeText: unknown) {
  Object.defineProperty(navigator, 'clipboard', {
    value: writeText ? { writeText } : undefined,
    configurable: true,
  })
}

describe('toClipboard', () => {
  const originalCommand = Object.getOwnPropertyDescriptor(document, 'execCommand')
  const originalModern = Object.getOwnPropertyDescriptor(navigator, 'clipboard')

  beforeEach(() => {
    setModern(undefined)
  })

  afterEach(() => {
    if (originalCommand) Object.defineProperty(document, 'execCommand', originalCommand)
    else delete (document as { execCommand?: unknown }).execCommand
    if (originalModern) Object.defineProperty(navigator, 'clipboard', originalModern)
    else delete (navigator as { clipboard?: unknown }).clipboard
    document.getSelection()?.removeAllRanges()
    document.body.innerHTML = ''
  })

  it('uses the modern clipboard when the page has one', async () => {
    const writeText = vi.fn(async () => {})
    setModern(writeText)
    const command = commandThatLies()
    setCommand(command)

    await expect(toClipboard('ls -la')).resolves.toBe(true)
    expect(writeText).toHaveBeenCalledWith('ls -la')
    expect(command).not.toHaveBeenCalled()
  })

  it('falls back to the command when the modern clipboard refuses', async () => {
    setModern(vi.fn(async () => Promise.reject(new Error('NotAllowedError'))))
    const clip: Clipboard = { text: null, prevented: false }
    setCommand(commandThatFires(clip))

    await expect(toClipboard('pwd')).resolves.toBe(true)
    expect(clip.text).toBe('pwd')
  })

  it('hands the exact text over on the copy event, whitespace and all', async () => {
    const clip: Clipboard = { text: null, prevented: false }
    setCommand(commandThatFires(clip))
    const text = '  indented\n\n\ttabbed   trailing  \nlast'

    await expect(toClipboard(text)).resolves.toBe(true)
    expect(clip.text).toBe(text)
    // Prevented, or the browser would follow up by copying its own idea of
    // the selection over the top.
    expect(clip.prevented).toBe(true)
  })

  it('does not believe a command that ran without copying', async () => {
    // Safari: the command is allowed, returns true, and copies nothing
    // because it saw no selection to copy.
    setCommand(commandThatLies())

    await expect(toClipboard('rm -rf ./build')).resolves.toBe(false)
  })

  it('does not believe the return value even when the event fired', async () => {
    // The listener taking the text is the proof; the command's own verdict
    // is not consulted either way.
    const clip: Clipboard = { text: null, prevented: false }
    setCommand(commandThatFires(clip, false))

    await expect(toClipboard('echo hi')).resolves.toBe(true)
    expect(clip.text).toBe('echo hi')
  })

  it('reports false when the command is refused outright', async () => {
    setCommand(
      vi.fn(() => {
        throw new Error('not allowed')
      }),
    )
    await expect(toClipboard('x')).resolves.toBe(false)
  })

  it('reports false when there is no command at all', async () => {
    delete (document as { execCommand?: unknown }).execCommand
    await expect(toClipboard('x')).resolves.toBe(false)
  })

  it('selects the text in the page while the command runs, then cleans up', async () => {
    // The page had its own selection, which is not ours to lose.
    const theirs = document.createElement('p')
    theirs.textContent = 'keep me'
    document.body.appendChild(theirs)
    const before = document.createRange()
    before.selectNodeContents(theirs)
    document.getSelection()?.addRange(before)

    let selectedDuring = ''
    let nodesDuring = 0
    setCommand(
      vi.fn(() => {
        selectedDuring = document.getSelection()?.toString() ?? ''
        nodesDuring = document.body.childElementCount
        return true
      }),
    )

    await toClipboard('the text')

    // A real, non-empty selection over the text — what Safari needs to run
    // the command at all.
    expect(selectedDuring).toBe('the text')
    expect(nodesDuring).toBe(2)
    // And afterwards, the page as it was.
    expect(document.body.childElementCount).toBe(1)
    expect(document.getSelection()?.toString()).toBe('keep me')
  })

  it('removes its listener once it is done', async () => {
    const clip: Clipboard = { text: null, prevented: false }
    setCommand(commandThatFires(clip))
    await toClipboard('first')

    // A copy the page does later on its own is none of ours.
    const later = new Event('copy', { bubbles: true, cancelable: true })
    document.body.dispatchEvent(later)
    expect(later.defaultPrevented).toBe(false)
  })
})

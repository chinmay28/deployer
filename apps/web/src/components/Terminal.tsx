import { useCallback, useEffect, useRef, useState } from 'react'
import { FitAddon } from '@xterm/addon-fit'
import { Terminal } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'
import { api } from '../api'
import { Sender, ShellStream, type ConnectionState } from '../lib/shell'
import type { ShellSession } from '../types'

/**
 * A terminal on a phone.
 *
 * xterm.js draws the screen — a terminal is a thirty-year-old protocol of
 * escape sequences and there is no point pretending otherwise, because `htop`
 * and `vim` and a coloured prompt are exactly what a shell is for. What is
 * Deployer's own work is everything around it, and all of that is about the
 * phone:
 *
 * The key bar. A phone keyboard has no Ctrl, no Esc, no Tab and no arrows,
 * which between them are most of what a shell is driven by. Ctrl is a sticky
 * modifier rather than a chord, because there is no chording on a touchscreen.
 *
 * The size. The soft keyboard covers half the screen, so the terminal is sized
 * to the visual viewport rather than the window, and the host is told the new
 * window each time — which is what makes a full-screen program redraw into the
 * space that is actually visible instead of behind the keyboard.
 */

/** Colours xterm needs as concrete values; it cannot read a CSS variable. Kept
 *  beside the two palettes in styles.css and picked the same way. */
const THEMES = {
  light: {
    background: '#ffffff',
    foreground: '#16161a',
    cursor: '#5b5bd6',
    cursorAccent: '#ffffff',
    selectionBackground: '#c8c8f5',
    black: '#16161a',
    red: '#c0362c',
    green: '#17864b',
    yellow: '#9a6100',
    blue: '#3a3ab8',
    magenta: '#8b3fa8',
    cyan: '#0f7490',
    white: '#d8d8e0',
    brightBlack: '#6b6b76',
    brightRed: '#e0503f',
    brightGreen: '#1fa860',
    brightYellow: '#b87c00',
    brightBlue: '#5b5bd6',
    brightMagenta: '#a855c8',
    brightCyan: '#1291b4',
    brightWhite: '#16161a',
  },
  dark: {
    background: '#0b0b0f',
    foreground: '#f2f2f5',
    cursor: '#8b8bf5',
    cursorAccent: '#0b0b0f',
    selectionBackground: '#33335a',
    black: '#2a2a34',
    red: '#f87171',
    green: '#4ade80',
    yellow: '#fbbf24',
    blue: '#8b8bf5',
    magenta: '#d8a0f0',
    cyan: '#5ed8f0',
    white: '#d8d8e0',
    brightBlack: '#9a9aa8',
    brightRed: '#fca5a5',
    brightGreen: '#86efac',
    brightYellow: '#fcd34d',
    brightBlue: '#a5a5f8',
    brightMagenta: '#e8bff8',
    brightCyan: '#a0e8f8',
    brightWhite: '#ffffff',
  },
} as const

/** Font sizes the size buttons step through. The smallest fits 80 columns on a
 *  phone held sideways, which is the width most command output assumes. */
const SIZES = [8, 9, 10, 11, 12, 13, 14, 16, 18]
const SIZE_KEY = 'deployer.terminal.fontSize'
const EXTRAS_KEY = 'deployer.terminal.extraKeys'

/** The keys a phone keyboard does not have, in the order a shell needs them. */
const KEYS: { label: string; bytes: number[]; title: string }[] = [
  { label: 'Esc', bytes: [0x1b], title: 'Escape' },
  { label: 'Tab', bytes: [0x09], title: 'Tab — complete' },
  { label: '^C', bytes: [0x03], title: 'Ctrl-C — stop what is running' },
  { label: '←', bytes: [0x1b, 0x5b, 0x44], title: 'Left' },
  { label: '↑', bytes: [0x1b, 0x5b, 0x41], title: 'Up — previous command' },
  { label: '↓', bytes: [0x1b, 0x5b, 0x42], title: 'Down' },
  { label: '→', bytes: [0x1b, 0x5b, 0x43], title: 'Right' },
]

/** The second row: what a shell is full of and a phone keyboard buries two
 *  layers down, then the keys for moving around a long line. */
const EXTRAS: { label: string; bytes: number[]; title: string }[] = [
  ...['|', '/', '\\', '~', '-', '_', '*', '$', '&', '#', '"', "'", '`', '{', '}'].map((ch) => ({
    label: ch,
    bytes: [ch.charCodeAt(0)],
    title: ch,
  })),
  { label: 'Home', bytes: [0x1b, 0x5b, 0x48], title: 'Start of the line' },
  { label: 'End', bytes: [0x1b, 0x5b, 0x46], title: 'End of the line' },
  { label: 'PgUp', bytes: [0x1b, 0x5b, 0x35, 0x7e], title: 'Page up' },
  { label: 'PgDn', bytes: [0x1b, 0x5b, 0x36, 0x7e], title: 'Page down' },
  { label: '^D', bytes: [0x04], title: 'Ctrl-D — end of input' },
  { label: '^Z', bytes: [0x1a], title: 'Ctrl-Z — suspend' },
  { label: '^L', bytes: [0x0c], title: 'Ctrl-L — clear the screen' },
]

export default function TerminalView({
  session,
  onSession,
  onExit,
  onError,
}: {
  session: ShellSession
  onSession: (s: ShellSession) => void
  onExit: (reason: string) => void
  onError: (message: string) => void
}) {
  const stageRef = useRef<HTMLDivElement>(null)
  const termRef = useRef<Terminal | null>(null)
  const fitRef = useRef<FitAddon | null>(null)
  const senderRef = useRef<Sender | null>(null)
  /** Armed modifiers. A ref as well as state because the handler that consumes
   *  them is installed once and must see the current value, not the one that
   *  was current when xterm was built. */
  const modsRef = useRef({ ctrl: false, alt: false })
  const [mods, setMods] = useState({ ctrl: false, alt: false })
  const [state, setState] = useState<ConnectionState>('connecting')
  const [extras, setExtras] = useState(() => window.localStorage.getItem(EXTRAS_KEY) === 'on')
  const [fontSize, setFontSize] = useState(readFontSize)
  /** What the terminal actually measured. Shown beside the size buttons,
   *  because "how many columns is this" is the only thing they are for. */
  const [cols, setCols] = useState(session.cols)

  const arm = useCallback((key: 'ctrl' | 'alt', on: boolean) => {
    modsRef.current = { ...modsRef.current, [key]: on }
    setMods({ ...modsRef.current })
  }, [])

  const send = useCallback((bytes: number[]) => {
    senderRef.current?.send(bytes)
    termRef.current?.focus()
  }, [])

  // Built once for the life of the session. Rebuilding it would throw the
  // screen away, and the screen is the thing being kept.
  useEffect(() => {
    const stage = stageRef.current
    if (!stage) return

    const dark = window.matchMedia('(prefers-color-scheme: dark)')
    const term = new Terminal({
      allowTransparency: false,
      cursorBlink: true,
      // A phone cannot show much at once, so the history is where anything
      // scrolled past has to be findable.
      scrollback: 5000,
      fontSize: readFontSize(),
      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
      lineHeight: 1.1,
      theme: dark.matches ? THEMES.dark : THEMES.light,
      // A tap on a link on a phone is a tap on a character next to it, so
      // nothing here is made clickable.
      convertEol: false,
    })
    const fit = new FitAddon()
    term.loadAddon(fit)
    term.open(stage)
    termRef.current = term
    fitRef.current = fit

    const sender = new Sender(session.id, onError)
    senderRef.current = sender

    // Everything typed, from the soft keyboard or a bluetooth one, arrives
    // here already encoded — so an armed Ctrl is applied to it rather than
    // being a key xterm has to know about.
    const typed = term.onData((data) => {
      const { ctrl, alt } = modsRef.current
      let bytes = Array.from(new TextEncoder().encode(data))
      if (ctrl && data.length === 1) {
        const code = controlCode(data)
        if (code !== null) bytes = [code]
      }
      if (alt) bytes = [0x1b, ...bytes]
      if (ctrl || alt) {
        modsRef.current = { ctrl: false, alt: false }
        setMods({ ctrl: false, alt: false })
      }
      sender.send(bytes)
    })
    // A pasted or dictated block arrives whole, and modifiers have no business
    // being applied to it.
    const pasted = term.onBinary((data) => {
      sender.send(Array.from(data, (ch) => ch.charCodeAt(0) & 0xff))
    })

    const onTheme = (e: MediaQueryListEvent) => {
      term.options.theme = e.matches ? THEMES.dark : THEMES.light
    }
    dark.addEventListener('change', onTheme)

    // The screen is asked for from the beginning: what Deployer kept is the
    // shell's history, and a client arriving at an hour-old session should see
    // what it has been doing rather than an empty rectangle.
    const stream = new ShellStream(session.id, 0, {
      onSession: (s) => {
        onSession(s)
        // Push this screen's size after every connect: a shell that was resized
        // by another screen, or by this one before it dropped, is otherwise
        // drawing into a window that is not the one here.
        pushSize(term.cols, term.rows)
      },
      onData: (bytes) => term.write(bytes),
      onGap: (missed) => {
        term.write(
          `\r\n\x1b[33m— ${missed.toLocaleString()} bytes scrolled past while this screen was away —\x1b[0m\r\n`,
        )
      },
      onState: setState,
      onExit,
    })
    stream.start()

    // iOS drops connections when the tab goes to the background and does not
    // always admit it, so coming back to the front is a cue to reopen rather
    // than to wait out a backoff on a connection that is already dead.
    const onVisible = () => {
      if (document.visibilityState !== 'visible') return
      stream.refresh()
      fitLater()
    }
    document.addEventListener('visibilitychange', onVisible)

    let sizeTimer = 0
    let lastSize = ''
    const pushSize = (cols: number, rows: number) => {
      const key = `${cols}x${rows}`
      if (key === lastSize) return
      lastSize = key
      window.clearTimeout(sizeTimer)
      // Debounced because turning a phone, or the keyboard sliding up, is a
      // stream of sizes and only the last one is the answer.
      sizeTimer = window.setTimeout(() => {
        api.shellResize(session.id, cols, rows).catch(() => {
          // A shell that has gone is reported by the stream, which is the one
          // place that should say so.
        })
      }, 150)
    }
    const resized = term.onResize(({ cols, rows }) => {
      setCols(cols)
      pushSize(cols, rows)
    })

    let fitTimer = 0
    const fitLater = () => {
      window.clearTimeout(fitTimer)
      fitTimer = window.setTimeout(() => {
        try {
          fit.fit()
        } catch {
          // fit throws while the stage has no size — mid-transition, or on a
          // screen being torn down. The next one will land.
        }
      }, 60)
    }
    const observer = new ResizeObserver(fitLater)
    observer.observe(stage)
    fitLater()

    return () => {
      document.removeEventListener('visibilitychange', onVisible)
      dark.removeEventListener('change', onTheme)
      window.clearTimeout(fitTimer)
      window.clearTimeout(sizeTimer)
      observer.disconnect()
      resized.dispose()
      typed.dispose()
      pasted.dispose()
      stream.stop()
      term.dispose()
      termRef.current = null
      fitRef.current = null
      senderRef.current = null
    }
    // Built for one session and torn down with it; the callbacks are held in
    // refs by the closures above rather than rebuilding the terminal.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session.id])

  // The soft keyboard covers the bottom half of the screen. Sizing to the
  // visual viewport is what keeps the prompt above it instead of behind it.
  useEffect(() => {
    const vv = window.visualViewport
    if (!vv) return
    const root = document.querySelector<HTMLElement>('.term-screen')
    if (!root) return
    const apply = () => {
      root.style.height = `${vv.height}px`
      root.style.transform = `translateY(${vv.offsetTop}px)`
    }
    apply()
    vv.addEventListener('resize', apply)
    vv.addEventListener('scroll', apply)
    return () => {
      vv.removeEventListener('resize', apply)
      vv.removeEventListener('scroll', apply)
      root.style.height = ''
      root.style.transform = ''
    }
  }, [])

  useEffect(() => {
    const term = termRef.current
    if (!term) return
    term.options.fontSize = fontSize
    window.localStorage.setItem(SIZE_KEY, String(fontSize))
    try {
      fitRef.current?.fit()
    } catch {
      // Same as above: a stage with no size yet.
    }
  }, [fontSize])

  const showExtras = (on: boolean) => {
    setExtras(on)
    window.localStorage.setItem(EXTRAS_KEY, on ? 'on' : 'off')
  }

  const step = (by: number) => {
    const at = SIZES.indexOf(fontSize)
    const next = SIZES[Math.min(SIZES.length - 1, Math.max(0, (at < 0 ? 3 : at) + by))]
    setFontSize(next)
  }

  const paste = async () => {
    try {
      const text = await navigator.clipboard.readText()
      if (text) senderRef.current?.type(text)
    } catch {
      onError('The clipboard is not readable here — it needs https or localhost.')
    }
    termRef.current?.focus()
  }

  return (
    <>
      <div className="term-stage" ref={stageRef} onClick={() => termRef.current?.focus()} />

      {state !== 'live' && (
        <div className={`term-state ${state}`} role="status">
          {state === 'connecting' && 'Connecting…'}
          {state === 'reconnecting' && 'Reconnecting — the shell is still open on the host'}
          {state === 'ended' && 'The shell has ended'}
        </div>
      )}

      <div className="term-keys">
        <div className="term-row">
          {KEYS.map((key) => (
            <KeyButton key={key.label} title={key.title} onPress={() => send(key.bytes)}>
              {key.label}
            </KeyButton>
          ))}
          <KeyButton
            title="Ctrl — then the next key"
            held={mods.ctrl}
            onPress={() => arm('ctrl', !modsRef.current.ctrl)}
          >
            Ctrl
          </KeyButton>
          <KeyButton title="More keys" held={extras} onPress={() => showExtras(!extras)}>
            •••
          </KeyButton>
        </div>

        {extras && (
          <div className="term-row">
            {/* Text size leads the row. It is the control this screen needs most
                and the one no desktop terminal has: how many columns fit is the
                difference between reading `ls -l` and unpicking it. */}
            <KeyButton title="Smaller text — more columns" onPress={() => step(-1)}>
              A−
            </KeyButton>
            <span className="term-cols" aria-label={`${cols} columns`}>
              {cols}c
            </span>
            <KeyButton title="Bigger text — fewer columns" onPress={() => step(1)}>
              A+
            </KeyButton>
            <KeyButton
              title="Alt — then the next key"
              held={mods.alt}
              onPress={() => arm('alt', !modsRef.current.alt)}
            >
              Alt
            </KeyButton>
            <KeyButton title="Paste from the clipboard" onPress={paste}>
              Paste
            </KeyButton>
            {EXTRAS.map((key) => (
              <KeyButton key={key.label} title={key.title} onPress={() => send(key.bytes)}>
                {key.label}
              </KeyButton>
            ))}
          </div>
        )}
      </div>
    </>
  )
}

/**
 * A key on the bar.
 *
 * It refuses focus rather than taking it, which is the whole trick: a button
 * that focused itself would dismiss the soft keyboard, and a key bar that
 * closes the keyboard every time you reach for Tab is worse than no key bar.
 */
function KeyButton({
  children,
  title,
  held,
  onPress,
}: {
  children: React.ReactNode
  title: string
  held?: boolean
  onPress: () => void
}) {
  return (
    <button
      type="button"
      className={`term-key ${held ? 'held' : ''}`}
      title={title}
      aria-label={title}
      aria-pressed={held}
      onPointerDown={(e) => e.preventDefault()}
      onMouseDown={(e) => e.preventDefault()}
      onClick={onPress}
    >
      {children}
    </button>
  )
}

/** What a key becomes with Ctrl held: the bottom five bits of it, which is what
 *  a terminal has always meant by a control character. */
function controlCode(ch: string): number | null {
  const c = ch.toLowerCase()
  if (c >= 'a' && c <= 'z') return c.charCodeAt(0) - 96
  const others: Record<string, number> = {
    '@': 0,
    ' ': 0,
    '[': 27,
    '\\': 28,
    ']': 29,
    '^': 30,
    '_': 31,
    '?': 127,
  }
  return c in others ? others[c] : null
}

function readFontSize(): number {
  const stored = Number(window.localStorage.getItem(SIZE_KEY))
  // 11 by default: big enough to read at arm's length, small enough that a
  // phone held upright fits the width most command output assumes it has.
  return SIZES.includes(stored) ? stored : 11
}

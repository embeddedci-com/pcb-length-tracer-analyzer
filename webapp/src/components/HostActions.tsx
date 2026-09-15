/**
 * The controls that only exist inside an editor.
 *
 * Each renders nothing in a browser, so a component can place one without
 * asking where it is running.
 */

import { useState, type ReactNode } from 'react'
import { Alert, Anchor, Button, Group, Text, Tooltip } from '@mantine/core'
import { useHost, type HostApplyResult } from '../lib/host'

/** A net's name that selects it on the open board when clicked. */
export function NetName({ net, children }: { net: string; children: ReactNode }) {
  const host = useHost()
  if (!host) return <>{children}</>
  return (
    <Tooltip label={`Select ${net} in ${host.name}`} openDelay={400}>
      <Anchor
        component="button"
        type="button"
        c="inherit"
        underline="hover"
        onClick={() => void host.selectNets([net])}
      >
        {children}
      </Anchor>
    </Tooltip>
  )
}

/** Selects a set of nets at once: a group's out-of-tolerance members, say. */
export function SelectNetsButton({
  nets,
  children,
  size = 'xs',
  variant = 'light',
}: {
  nets: string[]
  children: ReactNode
  size?: 'xs' | 'sm' | 'md'
  variant?: 'light' | 'default' | 'filled' | 'subtle'
}) {
  const host = useHost()
  const [busy, setBusy] = useState(false)
  if (!host || nets.length === 0) return null
  return (
    <Button
      size={size}
      variant={variant}
      loading={busy}
      onClick={() => {
        setBusy(true)
        void host.selectNets(nets).finally(() => setBusy(false))
      }}
    >
      {children}
    </Button>
  )
}

/** Reads the open board again. */
export function RescanButton() {
  const host = useHost()
  const [busy, setBusy] = useState(false)
  if (!host) return null
  return (
    <Tooltip label={`Read the board in ${host.name} again, with the edits you have made since`}>
      <Button
        size="sm"
        variant="default"
        loading={busy}
        onClick={() => {
          setBusy(true)
          void host.rescan().finally(() => setBusy(false))
        }}
      >
        Rescan
      </Button>
    </Tooltip>
  )
}

/**
 * Applies the session's result to the open board as one undoable edit.
 *
 * The server has already tuned its copy; this is the step that changes the
 * user's board, so it is a button of its own and says what it did.
 */
export function ApplyToBoard({ sessionId }: { sessionId: string }) {
  const host = useHost()
  const [busy, setBusy] = useState(false)
  const [done, setDone] = useState<HostApplyResult | null>(null)
  const [error, setError] = useState<string | null>(null)
  const applyToBoard = host?.applyToBoard
  if (!host || !applyToBoard) return null
  return (
    <>
      <Group gap="sm">
        <Button
          loading={busy}
          disabled={done !== null}
          onClick={() => {
            setBusy(true)
            setError(null)
            applyToBoard(sessionId)
              .then(setDone)
              .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
              .finally(() => setBusy(false))
          }}
        >
          {done ? `Applied in ${host.name}` : `Apply to the board in ${host.name}`}
        </Button>
      </Group>
      {done && (
        <Alert color="green" variant="light" title={`Applied in ${host.name}`}>
          <Text size="sm">
            {done.removed} track{done.removed === 1 ? '' : 's'} replaced by {done.added}, as one edit
            ("{done.message}"). Undo it in {host.name} to put the board back. Rescan to measure the
            board as it is now.
          </Text>
        </Alert>
      )}
      {error && (
        <Alert color="red" variant="light" title="Not applied">
          <Text size="sm">{error}</Text>
        </Alert>
      )}
    </>
  )
}

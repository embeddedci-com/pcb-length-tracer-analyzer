/**
 * Millimetres or thousandths of an inch.
 *
 * Boards are drawn in both. KiCad carries the choice per user and so does this:
 * a stack-up quoted in mm to somebody who thinks in mils is arithmetic they
 * have to do in their head against a datasheet, every time.
 *
 * The choice lives outside React because every formatter in the app reads it
 * and threading it through a dozen components as a prop would put it in the way
 * of everything. Components that display lengths subscribe with useUnit(), and
 * the pages do that once at the top: everything below them re-renders with it.
 */

import { useSyncExternalStore } from 'react'

export type Unit = 'mm' | 'mil'

/** One mil in millimetres. Exact, by definition of the inch. */
export const MM_PER_MIL = 0.0254

const KEY = 'pcb-trace-length-analyzer.unit'

function initial(): Unit {
  try {
    return localStorage.getItem(KEY) === 'mil' ? 'mil' : 'mm'
  } catch {
    // A browser with site data blocked still gets to read a board.
    return 'mm'
  }
}

let current: Unit = initial()
const listeners = new Set<() => void>()

export function getUnit(): Unit {
  return current
}

export function setUnit(u: Unit): void {
  if (u === current) return
  current = u
  try {
    localStorage.setItem(KEY, u)
  } catch {
    // Not being able to remember it is not a reason not to do it.
  }
  for (const l of listeners) l()
}

/** Subscribes a component to the choice, and returns it. */
export function useUnit(): Unit {
  return useSyncExternalStore(
    (onChange) => {
      listeners.add(onChange)
      return () => listeners.delete(onChange)
    },
    () => current,
    () => 'mm' as Unit,
  )
}

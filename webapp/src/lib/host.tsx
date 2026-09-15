/**
 * What the page can do when it is running inside an editor rather than a
 * browser tab.
 *
 * On embeddedci.com the board is an uploaded copy: the page can report on it
 * and hand a file back, and that is all. Inside the KiCad plugin the same page
 * sits beside the board the user has open, and three things become possible
 * that are impossible on the site -- pointing at a net on that board, applying
 * a result to it as an edit, and reading it again after the user has changed
 * it.
 *
 * The page asks for those through this context and shows the controls only
 * when a host provides them. Nothing here knows about KiCad: the plugin's shell
 * supplies the implementation, and the site supplies none.
 */

import { createContext, useContext, type ReactNode } from 'react'

/** What applying a result to the open board came to. */
export interface HostApplyResult {
  /** Items removed and added in the one commit. */
  removed: number
  added: number
  /** What the edit is called in the editor's undo history. */
  message: string
}

export interface BoardHost {
  /** The editor's name, for button labels: "Select in KiCad". */
  name: string
  /** Select these nets' copper on the open board and bring it into view. */
  selectNets(nets: string[]): Promise<void>
  /**
   * Make the session's last apply on the open board, as one undoable edit.
   *
   * Refused -- rejected, not partly done -- when any track it would replace is
   * no longer the track that was analysed.
   *
   * Optional: a host that does not offer it gets no Apply button. The KiCad
   * plugin leaves it out until writing to a live board has been tested there.
   */
  applyToBoard?(sessionId: string): Promise<HostApplyResult>
  /** Read the open board again, into a new session, and show that. */
  rescan(): Promise<void>
}

const HostContext = createContext<BoardHost | null>(null)

export function HostProvider({ host, children }: { host: BoardHost | null; children: ReactNode }) {
  return <HostContext.Provider value={host}>{children}</HostContext.Provider>
}

/** The editor the page is running in, or null in a browser. */
export function useHost(): BoardHost | null {
  return useContext(HostContext)
}

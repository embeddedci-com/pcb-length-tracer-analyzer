/**
 * What a browser has and jsdom does not.
 *
 * Mantine's components ask the platform questions jsdom cannot answer -- what
 * the colour scheme is, how big an element is, whether a media query matches --
 * and without answers they throw during render rather than failing an
 * assertion, which makes every test look broken for the same uninformative
 * reason. These are the smallest answers that let a component render.
 */

import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterEach, vi } from 'vitest'

// Mantine reads this on mount for its colour scheme and its responsive props.
Object.defineProperty(window, 'matchMedia', {
  writable: true,
  value: (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }),
})

// jsdom lays nothing out, so every element measures zero. Anything that scrolls
// or sizes itself needs these to exist rather than to be right.
window.ResizeObserver = class {
  observe() {}
  unobserve() {}
  disconnect() {}
} as unknown as typeof ResizeObserver

window.scrollTo = vi.fn() as unknown as typeof window.scrollTo

// Mantine's dropdowns keep the active option in view as you arrow through
// them; jsdom has no scrolling, and the missing method throws rather than
// doing nothing, which aborts the click that opened the list.
Element.prototype.scrollIntoView = vi.fn()

// Testing-library only registers its own teardown when vitest runs with
// globals; this harness does not, so without this the DOM from one test is
// still on the page during the next and every query finds two of everything.
afterEach(cleanup)

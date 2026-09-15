/**
 * What the tool does, before anybody hands it a board file.
 *
 * Kept short on purpose: what comes back, what it may change, what to upload
 * and what happens to the file.
 */

import { Badge, List, SimpleGrid } from '@mantine/core'
import { bytes } from '../lib/format'

export function WhatThisDoes({
  maxUploadBytes,
  sessionTTLSeconds,
}: {
  maxUploadBytes?: number
  sessionTTLSeconds?: number
}) {
  const hours = sessionTTLSeconds ? Math.round(sessionTTLSeconds / 3600) : null

  return (
    <SimpleGrid cols={{ base: 1, sm: 2 }} spacing="lg">
      <List size="sm" spacing={4} c="dimmed">
        <List.Item>Finds DDR, USB, PCIe, Ethernet, MIPI and other differential pairs.</List.Item>
        <List.Item>Measures each net against its target length.</List.Item>
        <List.Item>Shows what to add, what to shorten and what is not routed.</List.Item>
        <List.Item>
          Can add meanders for you{' '}
          <Badge size="xs" variant="light" color="orange">
            experimental
          </Badge>
        </List.Item>
      </List>
      <List size="sm" spacing={4} c="dimmed">
        <List.Item>
          Upload the .kicad_pcb, plus the .kicad_pro and .kicad_dru for your rules.
        </List.Item>
        <List.Item>Your file is never modified.</List.Item>
        <List.Item>Only you can see the board.</List.Item>
        <List.Item>
          Deleted after {hours ? `${hours} hour${hours === 1 ? '' : 's'}` : 'a few hours'}
          {maxUploadBytes ? `. Max ${bytes(maxUploadBytes)}.` : '.'}
        </List.Item>
      </List>
    </SimpleGrid>
  )
}

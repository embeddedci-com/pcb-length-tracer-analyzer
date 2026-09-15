/**
 * The upload step.
 *
 * It asks for three files, not one. The board alone is enough to measure
 * lengths, but without the .kicad_pro there are no net classes, so every
 * clearance falls back to the board minimum and the tool works to rules the
 * designer did not set; and without the .kicad_dru a board whose rules relax
 * clearance inside a BGA courtyard is read as having no room where it has
 * some. The form therefore asks for both up front and says why, rather than
 * letting the caveat appear on the report afterwards.
 */

import { useState } from 'react'
import {
  Alert,
  Button,
  Collapse,
  FileInput,
  Group,
  Stack,
  Text,
  TextInput,
  Title,
} from '@mantine/core'
import { bytes } from '../lib/format'

export interface UploadFormProps {
  maxUploadBytes?: number
  busy?: boolean
  error?: string | null
  onSubmit: (v: {
    board: File
    project: File | null
    rules: File | null
    netPrefix: string
    controller: string
  }) => void
}

export function UploadForm({ maxUploadBytes, busy, error, onSubmit }: UploadFormProps) {
  const [board, setBoard] = useState<File | null>(null)
  const [project, setProject] = useState<File | null>(null)
  const [rules, setRules] = useState<File | null>(null)
  const [netPrefix, setNetPrefix] = useState('')
  const [controller, setController] = useState('')
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [over, setOver] = useState(false)

  /**
   * Files dropped on the form, sorted by what they are.
   *
   * Dropping the whole project folder's three files at once is the point: they
   * belong together, they are always in the same directory, and picking them
   * one at a time through three file dialogs is three chances to bring the
   * wrong board's project file.
   */
  const take = (files: FileList | null) => {
    for (const f of Array.from(files ?? [])) {
      const name = f.name.toLowerCase()
      if (name.endsWith('.kicad_pcb')) setBoard(f)
      else if (name.endsWith('.kicad_pro')) setProject(f)
      else if (name.endsWith('.kicad_dru')) {
        setRules(f)
        setShowAdvanced(true)
      }
    }
  }

  const tooBig = !!(board && maxUploadBytes && board.size > maxUploadBytes)
  const wrongExt = !!(board && !board.name.toLowerCase().endsWith('.kicad_pcb'))

  return (
    <Stack
      gap="md"
      onDragOver={(e) => {
        e.preventDefault()
        setOver(true)
      }}
      onDragLeave={(e) => {
        // Only when the pointer has actually left the form, not when it
        // crosses from one child of it to another.
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setOver(false)
      }}
      onDrop={(e) => {
        e.preventDefault()
        setOver(false)
        take(e.dataTransfer?.files ?? null)
      }}
      style={
        over
          ? {
              outline: '2px dashed var(--mantine-color-blue-5)',
              outlineOffset: 8,
              borderRadius: 8,
            }
          : undefined
      }
    >
      <div>
        <Title order={3}>Analyze a board</Title>
        <Text size="sm" c="dimmed">
          Drop all files here at once.
        </Text>
      </div>

      <FileInput
        required
        label="Board"
        description=".kicad_pcb"
        placeholder="choose the .kicad_pcb"
        accept=".kicad_pcb"
        value={board}
        onChange={setBoard}
        error={
          wrongExt
            ? 'This needs to be a .kicad_pcb file'
            : tooBig && maxUploadBytes
              ? `That file is ${bytes(board.size)}; the limit is ${bytes(maxUploadBytes)}`
              : undefined
        }
      />

      <FileInput
        label="Project file"
        description=".kicad_pro, for your net classes"
        placeholder="choose the .kicad_pro"
        accept=".kicad_pro"
        value={project}
        onChange={setProject}
      />

      {board && !project && (
        <Alert color="yellow" variant="light" title="No project file">
          Lengths are still correct. Clearances fall back to the board minimum.
        </Alert>
      )}

      <Button variant="subtle" size="compact-sm" onClick={() => setShowAdvanced((v) => !v)}>
        {showAdvanced ? 'Fewer options' : 'More options'}
      </Button>
      <Collapse in={showAdvanced}>
        <Stack gap="sm">
          <FileInput
            label="Custom design rules"
            description=".kicad_dru, if the board has one"
            placeholder="choose the .kicad_dru"
            accept=".kicad_dru"
            value={rules}
            onChange={setRules}
          />
          <TextInput
            label="Net prefix"
            description="For example /ddr4/. Detected if empty."
            placeholder="worked out automatically"
            value={netPrefix}
            onChange={(e) => setNetPrefix(e.currentTarget.value)}
          />
          <TextInput
            label="Memory controller"
            description="For example U3. Detected if empty."
            placeholder="worked out automatically"
            value={controller}
            onChange={(e) => setController(e.currentTarget.value)}
          />
        </Stack>
      </Collapse>

      {error && (
        <Alert color="red" variant="light" title="That did not work">
          {error}
        </Alert>
      )}

      <Group justify="flex-end">
        <Button
          disabled={!board || tooBig || wrongExt || busy}
          loading={busy}
          onClick={() =>
            board && onSubmit({ board, project, rules, netPrefix: netPrefix.trim(), controller: controller.trim() })
          }
        >
          Read the board
        </Button>
      </Group>
    </Stack>
  )
}

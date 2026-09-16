/**
 * The landing page: upload a board, or pick up one already uploaded.
 */

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router'
import { Alert, Anchor, Button, Card, Group, Loader, Stack, Table, Text, Title } from '@mantine/core'
import { ApiError, type AnalyzerApi } from '../lib/analyzerApi'
import { UploadForm } from '../components/UploadForm'
import { WhatThisDoes } from '../components/WhatThisDoes'
import { HowWeMeasure } from '../components/HowWeMeasure'
import { SupportedChips } from '../components/SupportedChips'
import { bytes, expiresIn } from '../lib/format'
import { UnitToggle } from '../components/UnitToggle'
import { useUnit } from '../lib/units'

export function HomePage({ api }: { api: AnalyzerApi }) {
  useUnit()
  const navigate = useNavigate()
  const qc = useQueryClient()

  const defaults = useQuery({ queryKey: ['analyzer', 'defaults'], queryFn: () => api.defaults() })
  const sessions = useQuery({ queryKey: ['analyzer', 'sessions'], queryFn: () => api.listSessions() })

  const upload = useMutation({
    mutationFn: (v: Parameters<typeof api.upload>[0]) => api.upload(v),
    onSuccess: (res) => {
      void qc.invalidateQueries({ queryKey: ['analyzer', 'sessions'] })
      qc.setQueryData(['analyzer', 'session', res.session.id], res)
      void navigate(res.session.id)
    },
  })
  const remove = useMutation({
    mutationFn: (id: string) => api.deleteSession(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['analyzer', 'sessions'] }),
  })

  if (defaults.error instanceof ApiError && defaults.error.isUnauthorized) {
    return (
      <Alert color="yellow" variant="light" title="Sign in first">
        A board is filed under your account, so this needs you signed in.
      </Alert>
    )
  }

  const rows = sessions.data?.sessions ?? []

  return (
    <Stack gap="xl">
      <div>
        <Group justify="space-between" align="flex-start" wrap="nowrap">
          <Title order={2}>PCB trace length analyzer</Title>
          <UnitToggle />
        </Group>
        <Text c="dimmed" mb="md">
          Check trace length matching on a KiCad board.
        </Text>
        <WhatThisDoes
          maxUploadBytes={defaults.data?.max_upload_bytes}
          sessionTTLSeconds={defaults.data?.session_ttl_seconds}
        />
        <Stack gap="sm" mt="md">
          <HowWeMeasure />
          <SupportedChips presets={defaults.data?.presets} />
        </Stack>
      </div>

      <Card withBorder padding="lg">
        <UploadForm
          maxUploadBytes={defaults.data?.max_upload_bytes}
          busy={upload.isPending}
          error={upload.error ? upload.error.message : null}
          onSubmit={(v) =>
            upload.mutate({
              board: v.board,
              project: v.project,
              rules: v.rules,
              netPrefix: v.netPrefix,
              controller: v.controller,
            })
          }
        />
      </Card>

      {sessions.isLoading ? (
        <Group justify="center">
          <Loader size="sm" />
        </Group>
      ) : rows.length > 0 ? (
        <Card withBorder padding="md">
          <Title order={4} mb="sm">
            Your boards
          </Title>
          <Table.ScrollContainer minWidth={480}>
            <Table verticalSpacing={6}>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>File</Table.Th>
                  <Table.Th>Size</Table.Th>
                  <Table.Th>Expires in</Table.Th>
                  <Table.Th>Applied</Table.Th>
                  <Table.Th />
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {rows.map((s) => (
                  <Table.Tr key={s.id}>
                    <Table.Td>
                      <Anchor onClick={() => void navigate(s.id)}>{s.filename}</Anchor>
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm" c="dimmed">
                        {bytes(s.board_bytes)}
                      </Text>
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm" c="dimmed">
                        {expiresIn(s.expires_at)}
                      </Text>
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm" c="dimmed">
                        {s.applied ? `${s.applied.nets.length} nets` : 'no'}
                      </Text>
                    </Table.Td>
                    <Table.Td>
                      <Button
                        size="compact-xs"
                        variant="subtle"
                        color="red"
                        loading={remove.isPending && remove.variables === s.id}
                        onClick={() => remove.mutate(s.id)}
                      >
                        Remove
                      </Button>
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
        </Card>
      ) : null}
    </Stack>
  )
}

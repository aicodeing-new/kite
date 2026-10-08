import { useMemo, useState } from 'react'
import {
  AlertTriangle,
  CheckCircle2,
  ChevronDown,
  Loader2,
  XCircle,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { NodeWithMetrics } from '@/types/api'
import {
  cordonNode,
  drainNode,
  restoreNodeScheduling,
  taintNode,
  uncordonNode,
  untaintNode,
} from '@/lib/api'
import { translateError } from '@/lib/utils'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

type NodeOperation =
  | 'cordon'
  | 'uncordon'
  | 'drain'
  | 'taint'
  | 'untaint'
  | 'restore'
type ResultStatus = 'pending' | 'running' | 'success' | 'failed' | 'skipped'

interface OperationResult {
  node: string
  status: ResultStatus
  message?: string
}

interface NodeBatchActionsProps {
  selectedNodes: NodeWithMetrics[]
  clearSelection: () => void
  refresh: () => Promise<unknown> | void
}

const operationLabels: Record<NodeOperation, string> = {
  cordon: 'Cordon',
  uncordon: 'Uncordon',
  drain: 'Drain',
  taint: 'Add / Update Taint',
  untaint: 'Remove Taint',
  restore: 'Restore Scheduling',
}

async function runWithConcurrency<T>(
  items: T[],
  concurrency: number,
  worker: (item: T) => Promise<void>
) {
  let nextIndex = 0
  const workers = Array.from(
    { length: Math.min(Math.max(concurrency, 1), items.length) },
    async () => {
      while (nextIndex < items.length) {
        const item = items[nextIndex]
        nextIndex += 1
        await worker(item)
      }
    }
  )
  await Promise.all(workers)
}

function getNodeName(node: NodeWithMetrics) {
  return node.metadata?.name || ''
}

function isControlPlane(node: NodeWithMetrics) {
  const labels = node.metadata?.labels || {}
  return (
    labels['node-role.kubernetes.io/control-plane'] !== undefined ||
    labels['node-role.kubernetes.io/master'] !== undefined
  )
}

export function NodeBatchActions({
  selectedNodes,
  clearSelection,
  refresh,
}: NodeBatchActionsProps) {
  const { t } = useTranslation()
  const [operation, setOperation] = useState<NodeOperation>('cordon')
  const [open, setOpen] = useState(false)
  const [running, setRunning] = useState(false)
  const [completed, setCompleted] = useState(false)
  const [results, setResults] = useState<OperationResult[]>([])
  const [drainOptions, setDrainOptions] = useState({
    force: false,
    gracePeriod: 30,
    deleteLocalData: false,
    ignoreDaemonsets: true,
    concurrency: 1,
  })
  const [taintData, setTaintData] = useState({
    key: '',
    value: '',
    effect: 'NoSchedule' as 'NoSchedule' | 'PreferNoSchedule' | 'NoExecute',
  })
  const [untaintKey, setUntaintKey] = useState('')

  const validNodes = useMemo(
    () => selectedNodes.filter((node) => getNodeName(node)),
    [selectedNodes]
  )
  const controlPlaneNodes = useMemo(
    () => validNodes.filter(isControlPlane),
    [validNodes]
  )
  const eligibleNodeCount = useMemo(
    () =>
      validNodes.filter((node) => {
        if (operation === 'cordon') return !node.spec?.unschedulable
        if (operation === 'uncordon') return !!node.spec?.unschedulable
        return true
      }).length,
    [operation, validNodes]
  )

  const openOperation = (nextOperation: NodeOperation) => {
    setOperation(nextOperation)
    setResults([])
    setCompleted(false)
    setOpen(true)
  }

  const handleOpenChange = (nextOpen: boolean) => {
    if (running) return
    setOpen(nextOpen)
    if (!nextOpen && completed && !results.some((r) => r.status === 'failed')) {
      clearSelection()
    }
  }

  const updateResult = (
    nodeName: string,
    status: ResultStatus,
    message?: string
  ) => {
    setResults((current) =>
      current.map((result) =>
        result.node === nodeName ? { ...result, status, message } : result
      )
    )
  }

  const execute = async (retryNames?: string[]) => {
    if (operation === 'taint' && !taintData.key.trim()) {
      toast.error('Taint key is required')
      return
    }
    if (operation === 'untaint' && !untaintKey.trim()) {
      toast.error('Taint key is required')
      return
    }

    const applicableNodes = validNodes.filter((node) => {
      if (retryNames && !retryNames.includes(getNodeName(node))) return false
      if (operation === 'cordon') return !node.spec?.unschedulable
      if (operation === 'uncordon') return !!node.spec?.unschedulable
      return true
    })
    const applicableNames = new Set(applicableNodes.map(getNodeName))

    if (retryNames) {
      setResults((current) =>
        current.map((result) =>
          applicableNames.has(result.node)
            ? { node: result.node, status: 'pending' }
            : result
        )
      )
    } else {
      setResults(
        validNodes.map((node) => {
          const name = getNodeName(node)
          if (applicableNames.has(name))
            return { node: name, status: 'pending' }
          return {
            node: name,
            status: 'skipped',
            message:
              operation === 'cordon'
                ? 'Already cordoned'
                : 'Already schedulable',
          }
        })
      )
    }

    setRunning(true)
    setCompleted(false)
    let failedCount = 0
    const concurrency = operation === 'drain' ? drainOptions.concurrency : 3

    await runWithConcurrency(applicableNodes, concurrency, async (node) => {
      const nodeName = getNodeName(node)
      updateResult(nodeName, 'running')
      try {
        if (operation === 'cordon') await cordonNode(nodeName)
        if (operation === 'uncordon') await uncordonNode(nodeName)
        if (operation === 'drain') {
          const result = await drainNode(nodeName, {
            force: drainOptions.force,
            gracePeriod: drainOptions.gracePeriod,
            deleteLocalData: drainOptions.deleteLocalData,
            ignoreDaemonsets: drainOptions.ignoreDaemonsets,
          })
          updateResult(
            nodeName,
            'success',
            `${result.pods} pod${result.pods === 1 ? '' : 's'} evicted`
          )
          if (result.warnings) {
            const warning = Array.isArray(result.warnings)
              ? result.warnings.join('; ')
              : result.warnings
            toast.warning(`${nodeName}: ${warning}`)
          }
          return
        }
        if (operation === 'taint') await taintNode(nodeName, taintData)
        if (operation === 'untaint') {
          await untaintNode(nodeName, untaintKey.trim())
        }
        if (operation === 'restore') {
          const result = await restoreNodeScheduling(nodeName)
          const summary = [
            result.removedTaints > 0
              ? `${result.removedTaints} taint${result.removedTaints === 1 ? '' : 's'} removed`
              : null,
            result.uncordoned ? 'uncordoned' : null,
          ]
            .filter(Boolean)
            .join(', ')
          updateResult(nodeName, 'success', summary || 'already schedulable')
          return
        }
        updateResult(nodeName, 'success')
      } catch (error) {
        failedCount += 1
        updateResult(nodeName, 'failed', translateError(error, t))
      }
    })

    setRunning(false)
    setCompleted(true)
    await refresh()
    if (failedCount > 0) {
      toast.error(
        `${operationLabels[operation]} completed with ${failedCount} failure${failedCount === 1 ? '' : 's'}`
      )
    } else {
      toast.success(
        `${operationLabels[operation]} completed for ${applicableNodes.length} node${applicableNodes.length === 1 ? '' : 's'}`
      )
    }
  }

  const failedNames = results
    .filter((result) => result.status === 'failed')
    .map((result) => result.node)

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="outline" className="gap-2">
            Bulk actions
            <ChevronDown className="h-4 w-4" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuLabel>
            {validNodes.length} node{validNodes.length === 1 ? '' : 's'}{' '}
            selected
          </DropdownMenuLabel>
          <DropdownMenuSeparator />
          <DropdownMenuItem onSelect={() => openOperation('cordon')}>
            Cordon
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={() => openOperation('uncordon')}>
            Uncordon
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={() => openOperation('drain')}>
            Drain
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem onSelect={() => openOperation('taint')}>
            Add / Update Taint
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={() => openOperation('untaint')}>
            Remove Taint
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem onSelect={() => openOperation('restore')}>
            Restore Scheduling
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <Dialog open={open} onOpenChange={handleOpenChange}>
        <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-xl">
          <DialogHeader>
            <DialogTitle>{operationLabels[operation]} nodes</DialogTitle>
            <DialogDescription>
              Apply this operation to {eligibleNodeCount} of {validNodes.length}{' '}
              selected node{validNodes.length === 1 ? '' : 's'}.
              {eligibleNodeCount < validNodes.length &&
                ` ${validNodes.length - eligibleNodeCount} already in the requested state will be skipped.`}
              {operation === 'restore' &&
                ' This removes all taints and marks the nodes as schedulable so pods can be assigned to them.'}
            </DialogDescription>
          </DialogHeader>

          {controlPlaneNodes.length > 0 && (
            <Alert variant="destructive">
              <AlertTriangle className="h-4 w-4" />
              <AlertTitle>Control-plane nodes selected</AlertTitle>
              <AlertDescription>
                This selection includes {controlPlaneNodes.length} control-plane
                node{controlPlaneNodes.length === 1 ? '' : 's'}. Verify cluster
                availability before continuing.
              </AlertDescription>
            </Alert>
          )}

          {operation === 'drain' && (
            <div className="grid gap-4 py-2">
              <div className="grid grid-cols-2 gap-3">
                <div className="grid gap-2">
                  <Label htmlFor="batch-drain-grace-period">
                    Grace period (seconds)
                  </Label>
                  <Input
                    id="batch-drain-grace-period"
                    type="number"
                    min={0}
                    value={drainOptions.gracePeriod}
                    onChange={(event) =>
                      setDrainOptions((current) => ({
                        ...current,
                        gracePeriod: Math.max(0, Number(event.target.value)),
                      }))
                    }
                  />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="batch-drain-concurrency">Concurrency</Label>
                  <Input
                    id="batch-drain-concurrency"
                    type="number"
                    min={1}
                    max={5}
                    value={drainOptions.concurrency}
                    onChange={(event) =>
                      setDrainOptions((current) => ({
                        ...current,
                        concurrency: Math.min(
                          5,
                          Math.max(1, Number(event.target.value))
                        ),
                      }))
                    }
                  />
                </div>
              </div>
              <label className="flex items-center gap-2 text-sm">
                <Checkbox
                  checked={drainOptions.ignoreDaemonsets}
                  onCheckedChange={(checked) =>
                    setDrainOptions((current) => ({
                      ...current,
                      ignoreDaemonsets: checked === true,
                    }))
                  }
                />
                Ignore DaemonSet-managed pods
              </label>
              <label className="flex items-center gap-2 text-sm">
                <Checkbox
                  checked={drainOptions.deleteLocalData}
                  onCheckedChange={(checked) =>
                    setDrainOptions((current) => ({
                      ...current,
                      deleteLocalData: checked === true,
                    }))
                  }
                />
                Delete local data (emptyDir)
              </label>
              <label className="flex items-center gap-2 text-sm">
                <Checkbox
                  checked={drainOptions.force}
                  onCheckedChange={(checked) =>
                    setDrainOptions((current) => ({
                      ...current,
                      force: checked === true,
                    }))
                  }
                />
                Force eviction of unmanaged pods
              </label>
            </div>
          )}

          {operation === 'taint' && (
            <div className="grid gap-4 py-2">
              <div className="grid gap-2">
                <Label htmlFor="batch-taint-key">Key</Label>
                <Input
                  id="batch-taint-key"
                  value={taintData.key}
                  onChange={(event) =>
                    setTaintData((current) => ({
                      ...current,
                      key: event.target.value,
                    }))
                  }
                  placeholder="example.com/workload"
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="batch-taint-value">Value</Label>
                <Input
                  id="batch-taint-value"
                  value={taintData.value}
                  onChange={(event) =>
                    setTaintData((current) => ({
                      ...current,
                      value: event.target.value,
                    }))
                  }
                />
              </div>
              <div className="grid gap-2">
                <Label>Effect</Label>
                <Select
                  value={taintData.effect}
                  onValueChange={(effect: typeof taintData.effect) =>
                    setTaintData((current) => ({ ...current, effect }))
                  }
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="NoSchedule">NoSchedule</SelectItem>
                    <SelectItem value="PreferNoSchedule">
                      PreferNoSchedule
                    </SelectItem>
                    <SelectItem value="NoExecute">NoExecute</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>
          )}

          {operation === 'untaint' && (
            <div className="grid gap-2 py-2">
              <Label htmlFor="batch-untaint-key">Taint key</Label>
              <Input
                id="batch-untaint-key"
                value={untaintKey}
                onChange={(event) => setUntaintKey(event.target.value)}
                placeholder="example.com/workload"
              />
            </div>
          )}

          {results.length > 0 && (
            <div className="max-h-64 space-y-2 overflow-y-auto rounded-md border p-3">
              {results.map((result) => (
                <div
                  key={result.node}
                  className="flex items-start justify-between gap-3 text-sm"
                >
                  <div className="min-w-0">
                    <div className="truncate font-mono">{result.node}</div>
                    {result.message && (
                      <div className="text-xs text-muted-foreground">
                        {result.message}
                      </div>
                    )}
                  </div>
                  <Badge
                    variant={
                      result.status === 'failed' ? 'destructive' : 'outline'
                    }
                    className="shrink-0 gap-1"
                  >
                    {result.status === 'running' && (
                      <Loader2 className="h-3 w-3 animate-spin" />
                    )}
                    {result.status === 'success' && (
                      <CheckCircle2 className="h-3 w-3 text-emerald-500" />
                    )}
                    {result.status === 'failed' && (
                      <XCircle className="h-3 w-3" />
                    )}
                    {result.status}
                  </Badge>
                </div>
              ))}
            </div>
          )}

          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => handleOpenChange(false)}
              disabled={running}
            >
              {completed ? 'Close' : 'Cancel'}
            </Button>
            {failedNames.length > 0 && !running ? (
              <Button onClick={() => execute(failedNames)}>
                Retry failed ({failedNames.length})
              </Button>
            ) : (
              <Button
                variant={operation === 'drain' ? 'destructive' : 'default'}
                onClick={() => execute()}
                disabled={running || completed || eligibleNodeCount === 0}
              >
                {running && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
                {running
                  ? 'Running...'
                  : `${operationLabels[operation]} ${eligibleNodeCount} node${
                      eligibleNodeCount === 1 ? '' : 's'
                    }`}
              </Button>
            )}
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}

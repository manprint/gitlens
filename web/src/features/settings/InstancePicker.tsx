import { NativeSelect } from '@/components/ui/native-select'
import { instanceLabel, type NamedInstance } from '@/lib/names'

import type { PickableInstance } from './usePickedInstance'

export function InstancePicker<T extends PickableInstance & NamedInstance>({
  instances,
  label,
  onPick,
  picked,
}: {
  instances: readonly T[]
  label: string
  onPick: (id: string) => void
  picked: T | undefined
}) {
  if (instances.length < 2) return null
  return (
    <label className="flex items-center gap-2 text-sm">
      <span className="text-muted-foreground">Instance</span>
      <NativeSelect
        aria-label={label}
        className="min-[1600px]:h-8"
        onChange={(event) => onPick(event.target.value)}
        value={picked?.instance_id ?? ''}
      >
        {instances.map((instance) => (
          <option key={instance.instance_id} value={instance.instance_id}>
            {instanceLabel(instance)}
          </option>
        ))}
      </NativeSelect>
    </label>
  )
}

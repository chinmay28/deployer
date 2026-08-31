import { Meter } from './ui'
import { primaryDisk } from '../lib/disk'
import { bytes, percent } from '../lib/format'
import type { Sample } from '../types'

/** The three bars a host is read by at a glance — CPU, memory and its main
 *  disk — shared by every card that shows a host in a list. */
export function HostMeters({ sample }: { sample: Sample }) {
  const disk = primaryDisk(sample.disks)
  return (
    <div className="meters">
      <Meter label="CPU" value={sample.cpuPct} display={`${Math.round(sample.cpuPct)}%`} />
      <Meter
        label="Memory"
        used={sample.memUsed}
        total={sample.memTotal}
        display={`${Math.round(percent(sample.memUsed, sample.memTotal))}%`}
      />
      {disk ? (
        <Meter
          label={`Disk ${disk.mount}`}
          used={disk.usedBytes}
          total={disk.totalBytes}
          display={bytes(disk.totalBytes - disk.usedBytes) + ' free'}
        />
      ) : (
        <Meter label="Disk" value={0} display="—" />
      )}
    </div>
  )
}

import type { Disk } from '../types'

/** Trees a real disk is never mounted under. A UEFI host reports
 *  /sys/firmware/efi/efivars — a few hundred kilobytes of NVRAM — through df,
 *  and older readings stored before the server learned to drop it are still in
 *  the history, so the phone filters them again on the way out. */
const PSEUDO_MOUNTS = ['/proc', '/sys', '/dev', '/run']

function isReal(disk: Disk): boolean {
  return (
    disk.totalBytes > 0 &&
    !PSEUDO_MOUNTS.some((prefix) => disk.mount === prefix || disk.mount.startsWith(`${prefix}/`))
  )
}

/** The one filesystem to show where there is room for only one: the root, or
 *  failing that the largest real store the host reported. */
export function primaryDisk(disks: Disk[] | undefined): Disk | undefined {
  const real = (disks ?? []).filter(isReal)
  return (
    real.find((disk) => disk.mount === '/') ??
    real.reduce<Disk | undefined>(
      (best, disk) => (!best || disk.totalBytes > best.totalBytes ? disk : best),
      undefined,
    )
  )
}

/** Every filesystem worth listing, primary first. */
export function realDisks(disks: Disk[] | undefined): Disk[] {
  const real = (disks ?? []).filter(isReal)
  const primary = primaryDisk(disks)
  return primary ? [primary, ...real.filter((disk) => disk !== primary)] : real
}

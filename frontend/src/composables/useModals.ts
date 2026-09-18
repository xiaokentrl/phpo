// useModals：模态打开入口，函数名对齐原型 open*Modal
// 视图与命令面板（T108）共用；危险确认在此组装 i18n 文案 + preflight/runTask 回调。
import { useModalStore } from '@/stores/modalStore'
import { useAppState } from '@/stores/appState'
import { usePreflight } from './usePreflight'
import { useI18n } from './useI18n'
import { toast } from './useToast'
import { runTask, type TaskMeta } from './useTask'
import { SVC_META } from '@/constants/service'
import InstallModal from '@/components/business/InstallModal.vue'
import SiteAddModal from '@/components/business/SiteAddModal.vue'
import RewriteModal from '@/components/business/RewriteModal.vue'
import ConfigModal from '@/components/business/ConfigModal.vue'
import PhpExtensionsModal from '@/components/business/PhpExtensionsModal.vue'
import SiteConfigModal from '@/components/business/SiteConfigModal.vue'
import ThemePickerModal from '@/components/business/ThemePickerModal.vue'
import HomeSetupWizard from '@/components/business/HomeSetupWizard.vue'
import DangerConfirm from '@/components/business/DangerConfirm.vue'
import type { ServiceKind } from '@/types'

export function useModals() {
  const modal = useModalStore()
  const app = useAppState()
  const { t } = useI18n()
  const { preflight } = usePreflight()

  const kindMeta = (k: string) => SVC_META[k as ServiceKind]

  function openInstallModal(kind: string): void {
    // 原型 2879：工作目录未就绪 → 先走装机向导，完成后再接续安装
    if (!app.dirReady.PHPO_HOME) { openHomeSetupWizard(() => modal.open(InstallModal, { kind })); return }
    modal.open(InstallModal, { kind })
  }
  function openSiteAddModal(): void {
    // 原型 3103：工作目录未就绪 → 先走装机向导，完成后再接续建站
    if (!app.dirReady.PHPO_HOME) { openHomeSetupWizard(() => modal.open(SiteAddModal, {})); return }
    modal.open(SiteAddModal, {})
  }
  function openRewriteModal(domain: string): void {
    modal.open(RewriteModal, { domain })
  }
  function openSiteConfigModal(domain: string): void {
    modal.open(SiteConfigModal, { domain })
  }
  function openConfigModal(kind: string, version: string): void {
    modal.open(ConfigModal, { kind, version })
  }
  function openPhpExtensionsModal(version: string): void {
    modal.open(PhpExtensionsModal, { version })
  }
  function openThemePicker(): void {
    modal.open(ThemePickerModal, {})
  }
  function openHomeSetupWizard(onReady?: () => void): void {
    modal.open(HomeSetupWizard, { onReady })
  }

  function openUninstallModal(kind: string, version: string): void {
    const meta = kindMeta(kind)
    modal.open(DangerConfirm, {
      title: t('danger.uninstall.title', { name: t(meta.titleKey), version }),
      description: t('danger.uninstall.desc'),
      warnings: [
        { text: t('danger.uninstall.warn1', { kind, version }) },
        { text: t('danger.uninstall.warn2', { kind, version }) },
        { text: t('danger.uninstall.warn3'), keep: true },
        { text: t('danger.uninstall.warn4'), keep: true },
      ],
      checkbox: { label: t('danger.uninstall.check', { kind, version }) },
      cliPreview: `phpo ${kind} uninstall ${version}`,
      confirmLabel: t('danger.uninstall.confirm'),
      onConfirm: () => {
        const check = preflight('uninstall', { kind, version })
        if (!check.ok) { toast(check.errors.join('\n'), 'err', 4600); return }
        runTask([kind, 'uninstall', version], `${t('svc.uninstall')} ${t(meta.titleKey)} ${version}`, { type: 'uninstall', kind, version })
      },
    })
  }

  function openSiteRemoveModal(domain: string): void {
    const sitesRoot = app.env.NGINX_SITES_ROOT || `${app.env.PHPO_HOME}/nginx/sites`
    modal.open(DangerConfirm, {
      title: t('danger.siteRemove.title', { domain }),
      description: t('danger.siteRemove.desc'),
      warnings: [
        { text: t('danger.siteRemove.warn1', { domain, root: sitesRoot }) },
        { text: t('danger.siteRemove.warn2'), keep: true },
        { text: t('danger.siteRemove.warn3'), keep: true },
        { text: t('danger.siteRemove.warn4'), keep: true },
      ],
      checkbox: { label: t('danger.siteRemove.check') },
      cliPreview: `phpo site remove ${domain}`,
      confirmLabel: t('danger.siteRemove.confirm'),
      onConfirm: () => {
        const check = preflight('site-remove', { domain })
        if (!check.ok) { toast(check.errors.join('\n'), 'err', 4600); return }
        runTask(['site', 'remove', domain], `${t('sites.actions.delete')} ${domain}`, { type: 'site-remove', domain })
      },
    })
  }

  function openDeleteBackupModal(file: string): void {
    const b = app.backups.find((x) => x.file === file)
    modal.open(DangerConfirm, {
      title: t('danger.backupDel.title'),
      description: file,
      warnings: [
        { text: t('danger.backupDel.warn1', { size: b?.size || '', time: b?.at || '' }) },
        { text: t('danger.backupDel.warn2') },
        { text: t('danger.backupDel.warn3'), keep: true },
      ],
      checkbox: { label: t('danger.backupDel.check') },
      cliPreview: `rm ${app.env.BACKUP_ROOT}/${file}`,
      confirmLabel: t('danger.backupDel.confirm'),
      onConfirm: () => {
        const check = preflight('backup-delete', { file })
        if (!check.ok) { toast(check.errors.join('\n'), 'err', 4600); return }
        const i = app.backups.findIndex((x) => x.file === file)
        if (i >= 0) app.backups.splice(i, 1)
        toast(`✓ ${file}`, 'ok', 2200)
      },
    })
  }

  function openOfflinePruneModal(svc: string, ver: string, size: string): void {
    modal.open(DangerConfirm, {
      title: t('danger.offlinePrune.title', { svc, ver }),
      description: t('danger.offlinePrune.desc'),
      warnings: [
        { text: t('danger.offlinePrune.warn1', { svc, ver, size }) },
        { text: t('danger.offlinePrune.warn2') },
        { text: t('danger.offlinePrune.warn3'), keep: true },
        { text: t('danger.offlinePrune.warn4'), keep: true },
      ],
      checkbox: { label: t('danger.offlinePrune.check') },
      cliPreview: `phpo offline prune ${svc} ${ver}`,
      confirmLabel: t('danger.offlinePrune.confirm'),
      onConfirm: () => {
        const check = preflight('offline-prune', { svc, ver })
        if (!check.ok) { toast(check.errors.join('\n'), 'err', 4600); return }
        runTask(['offline', 'prune', svc, ver], `Prune ${svc}/${ver}`, { type: 'offline-prune', svc, ver })
      },
    })
  }

  function openRestoreModal(file: string): void {
    const b = app.backups.find((x) => x.file === file)
    modal.open(DangerConfirm, {
      title: t('danger.restore.title'),
      description: t('danger.restore.desc', { file, size: b?.size || '' }),
      warnings: [
        { text: t('danger.restore.warn1') },
        { text: t('danger.restore.warn2') },
        { text: t('danger.restore.warn3') },
        { text: t('danger.restore.warn4'), keep: true },
      ],
      checkbox: { label: t('danger.restore.check') },
      cliPreview: `phpo restore ${app.env.BACKUP_ROOT}/${file}`,
      confirmLabel: t('danger.restore.confirm'),
      onConfirm: () => {
        const check = preflight('restore', { file })
        if (!check.ok) { toast(check.errors.join('\n'), 'err', 4600); return }
        runTask(['restore', `${app.env.BACKUP_ROOT}/${file}`], `Restore ${file}`, { type: 'restore', file })
      },
    })
  }

  // runGuardedTask：忠实原型 1939–1956。预检 → 错误 toast / 有警告则危险确认 → runTask。
  function runGuardedTask(
    action: string,
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    ctx: any = {},
    args: string[] = [],
    label?: string,
    meta?: TaskMeta,
    opts: { skipWarnings?: boolean } = {},
  ): boolean {
    const check = preflight(action, ctx)
    if (!check.ok) { toast(check.errors.join('\n'), 'err', 4600); return false }
    if (!opts.skipWarnings && check.warnings.length) {
      modal.open(DangerConfirm, {
        title: label || String(action),
        warnings: check.warnings.map((w) => ({ text: w })),
        confirmLabel: t('common.confirm'),
        onConfirm: () => runTask(args, label, meta),
      })
      return true
    }
    runTask(args, label, meta)
    return true
  }

  return {
    openInstallModal, openSiteAddModal, openRewriteModal, openSiteConfigModal,
    openConfigModal, openPhpExtensionsModal, openThemePicker, openHomeSetupWizard,
    openUninstallModal, openSiteRemoveModal, openDeleteBackupModal, openOfflinePruneModal, openRestoreModal,
    runGuardedTask,
  }
}

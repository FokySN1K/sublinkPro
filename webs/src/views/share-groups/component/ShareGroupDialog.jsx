import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import Dialog from '@mui/material/Dialog';
import DialogTitle from '@mui/material/DialogTitle';
import DialogContent from '@mui/material/DialogContent';
import DialogActions from '@mui/material/DialogActions';
import Button from '@mui/material/Button';
import Stack from '@mui/material/Stack';
import TextField from '@mui/material/TextField';
import MenuItem from '@mui/material/MenuItem';
import FormControlLabel from '@mui/material/FormControlLabel';
import Switch from '@mui/material/Switch';
import Checkbox from '@mui/material/Checkbox';
import FormGroup from '@mui/material/FormGroup';
import Typography from '@mui/material/Typography';
import Grid from '@mui/material/Grid';
import Divider from '@mui/material/Divider';

import { createShareGroup, updateShareGroup } from 'api/shareGroups';
import { getTemplates } from 'api/templates';

const GB = 1024 * 1024 * 1024;
const TEMPLATE_KEYS = ['auto', 'mihomo', 'mihomo_snkp', 'surge', 'v2ray'];
const DEVICE_MODES = ['open', 'auto_bind', 'approve'];

const DEFAULT_FORM = {
  name: '',
  description: '',
  subscription_id: '',
  enabled: true,
  hide_config: false,
  mihomo_snkp: true,
  allow_legacy_clients: true,
  templates: ['auto', 'mihomo', 'mihomo_snkp', 'surge', 'v2ray'],
  clash_template: '',
  surge_template: '',
  device_mode: 'open',
  max_devices: 0,
  traffic_limit_gb: '',
  expire_type: 0,
  expire_days: 30,
  expire_at: '',
  telemetry_enabled: true,
  report_interval_sessions: 3600,
  report_interval_nodes: 3600,
  report_interval_reliability: 3600
};

export default function ShareGroupDialog({ open, group, pools, onClose, onSaved, showMessage }) {
  const { t } = useTranslation();
  const [form, setForm] = useState(DEFAULT_FORM);
  const [saving, setSaving] = useState(false);
  const [templateFiles, setTemplateFiles] = useState({ clash: [], surge: [] });

  const isEditing = Boolean(group?.id);

  // 模板文件列表：分组级选择直接决定该分组链接渲染使用的模板。
  useEffect(() => {
    if (!open) return undefined;
    let cancelled = false;
    getTemplates()
      .then((res) => {
        if (cancelled) return;
        const items = res.data?.items || res.data || [];
        setTemplateFiles({
          clash: items.filter((item) => (item.category || 'clash') === 'clash'),
          surge: items.filter((item) => item.category === 'surge')
        });
      })
      .catch(() => {
        if (!cancelled) setTemplateFiles({ clash: [], surge: [] });
      });
    return () => {
      cancelled = true;
    };
  }, [open]);

  useEffect(() => {
    if (!open) return;
    if (group) {
      setForm({
        name: group.name || '',
        description: group.description || '',
        subscription_id: group.subscription_id || '',
        enabled: group.enabled !== false,
        hide_config: Boolean(group.hide_config),
        mihomo_snkp: group.mihomo_snkp !== false,
        allow_legacy_clients: group.allow_legacy_clients !== false,
        templates: group.template_list?.length ? group.template_list : DEFAULT_FORM.templates,
        clash_template: group.clash_template || '',
        surge_template: group.surge_template || '',
        device_mode: group.device_mode || 'open',
        max_devices: group.max_devices || 0,
        traffic_limit_gb: group.traffic_limit_bytes > 0 ? (group.traffic_limit_bytes / GB).toFixed(2) : '',
        expire_type: group.expire_type || 0,
        expire_days: group.expire_days || 30,
        expire_at: group.expire_at ? group.expire_at.substring(0, 16) : '',
        telemetry_enabled: Boolean(group.telemetry_enabled),
        report_interval_sessions: group.report_interval_sessions || 3600,
        report_interval_nodes: group.report_interval_nodes || 3600,
        report_interval_reliability: group.report_interval_reliability || 3600
      });
    } else {
      setForm({ ...DEFAULT_FORM, subscription_id: pools[0]?.ID || '' });
    }
  }, [open, group, pools]);

  const update = (patch) => setForm((prev) => ({ ...prev, ...patch }));

  const toggleTemplate = (key) => {
    setForm((prev) => {
      const exists = prev.templates.includes(key);
      return {
        ...prev,
        templates: exists ? prev.templates.filter((item) => item !== key) : [...prev.templates, key]
      };
    });
  };

  const effectiveTemplates = useMemo(() => {
    if (form.hide_config) return ['mihomo_snkp'];
    return form.templates;
  }, [form.hide_config, form.templates]);

  const handleSave = async () => {
    if (!form.name.trim()) {
      showMessage?.(t('shareGroups.messages.nameRequired'), 'warning');
      return;
    }
    if (!form.subscription_id) {
      showMessage?.(t('shareGroups.messages.poolRequired'), 'warning');
      return;
    }
    setSaving(true);
    try {
      const payload = {
        name: form.name.trim(),
        description: form.description.trim(),
        subscription_id: Number(form.subscription_id),
        enabled: form.enabled,
        hide_config: form.hide_config,
        mihomo_snkp: form.mihomo_snkp,
        allow_legacy_clients: form.allow_legacy_clients,
        templates: form.templates,
        clash_template: form.clash_template,
        surge_template: form.surge_template,
        device_mode: form.device_mode,
        max_devices: Number(form.max_devices) || 0,
        traffic_limit_bytes: Math.round((Number(form.traffic_limit_gb) || 0) * GB),
        expire_type: Number(form.expire_type) || 0,
        expire_days: Number(form.expire_days) || 0,
        expire_at: form.expire_type === 2 ? form.expire_at : '',
        telemetry_enabled: form.telemetry_enabled,
        report_interval_sessions: Number(form.report_interval_sessions) || 3600,
        report_interval_nodes: Number(form.report_interval_nodes) || 3600,
        report_interval_reliability: Number(form.report_interval_reliability) || 3600
      };
      if (isEditing) {
        await updateShareGroup({ id: group.id, ...payload });
        showMessage?.(t('shareGroups.messages.updateSuccess'), 'success');
      } else {
        await createShareGroup(payload);
        showMessage?.(t('shareGroups.messages.createSuccess'), 'success');
      }
      onSaved?.();
      onClose?.();
    } catch (error) {
      showMessage?.(error.response?.data?.msg || t('shareGroups.messages.saveFailed'), 'error');
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onClose={onClose} maxWidth="md" fullWidth>
      <DialogTitle>{isEditing ? t('shareGroups.dialog.editTitle') : t('shareGroups.dialog.createTitle')}</DialogTitle>
      <DialogContent dividers>
        <Stack spacing={2.5} sx={{ pt: 1 }}>
          <Grid container spacing={2}>
            <Grid size={{ xs: 12, md: 6 }}>
              <TextField
                fullWidth
                required
                label={t('shareGroups.fields.name')}
                value={form.name}
                onChange={(e) => update({ name: e.target.value })}
              />
            </Grid>
            <Grid size={{ xs: 12, md: 6 }}>
              <TextField
                fullWidth
                select
                required
                label={t('shareGroups.fields.pool')}
                value={form.subscription_id}
                onChange={(e) => update({ subscription_id: e.target.value })}
                helperText={t('shareGroups.fields.poolHint')}
              >
                {pools.map((pool) => (
                  <MenuItem key={pool.ID} value={pool.ID}>
                    {pool.Name || `#${pool.ID}`}
                  </MenuItem>
                ))}
              </TextField>
            </Grid>
            <Grid size={12}>
              <TextField
                fullWidth
                multiline
                minRows={2}
                label={t('shareGroups.fields.description')}
                value={form.description}
                onChange={(e) => update({ description: e.target.value })}
              />
            </Grid>
          </Grid>

          <Divider textAlign="left">
            <Typography variant="caption" color="text.secondary">
              {t('shareGroups.sections.delivery')}
            </Typography>
          </Divider>

          <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2} flexWrap="wrap">
            <FormControlLabel
              control={<Switch checked={form.enabled} onChange={(e) => update({ enabled: e.target.checked })} />}
              label={t('shareGroups.fields.enabled')}
            />
            <FormControlLabel
              control={<Switch checked={form.hide_config} onChange={(e) => update({ hide_config: e.target.checked })} />}
              label={t('shareGroups.fields.hideConfig')}
            />
            <FormControlLabel
              control={<Switch checked={form.mihomo_snkp} onChange={(e) => update({ mihomo_snkp: e.target.checked })} />}
              label={t('shareGroups.fields.mihomoSnkp')}
            />
            <FormControlLabel
              control={<Switch checked={form.allow_legacy_clients} onChange={(e) => update({ allow_legacy_clients: e.target.checked })} />}
              label={t('shareGroups.fields.allowLegacy')}
            />
          </Stack>
          <Typography variant="caption" color="text.secondary">
            {t('shareGroups.fields.hideConfigHint')}
          </Typography>

          <div>
            <Typography variant="subtitle2" sx={{ mb: 0.5 }}>
              {t('shareGroups.fields.templates')}
            </Typography>
            <FormGroup row>
              {TEMPLATE_KEYS.map((key) => (
                <FormControlLabel
                  key={key}
                  disabled={form.hide_config}
                  control={<Checkbox checked={form.templates.includes(key)} onChange={() => toggleTemplate(key)} />}
                  label={t(`shareGroups.templates.${key}`)}
                />
              ))}
            </FormGroup>
            {form.hide_config && (
              <Typography variant="caption" color="warning.main">
                {t('shareGroups.fields.hideConfigForces', { templates: effectiveTemplates.join(', ') })}
              </Typography>
            )}
          </div>

          <Grid container spacing={2}>
            <Grid size={{ xs: 12, md: 6 }}>
              <TextField
                fullWidth
                select
                label={t('shareGroups.fields.clashTemplate')}
                value={form.clash_template}
                onChange={(e) => update({ clash_template: e.target.value })}
                helperText={t('shareGroups.fields.templateInheritHint')}
              >
                <MenuItem value="">{t('shareGroups.fields.templateInherit')}</MenuItem>
                {templateFiles.clash.map((item) => (
                  <MenuItem key={item.file} value={item.file}>
                    {item.file}
                  </MenuItem>
                ))}
              </TextField>
            </Grid>
            <Grid size={{ xs: 12, md: 6 }}>
              <TextField
                fullWidth
                select
                label={t('shareGroups.fields.surgeTemplate')}
                value={form.surge_template}
                onChange={(e) => update({ surge_template: e.target.value })}
                helperText={t('shareGroups.fields.templateInheritHint')}
              >
                <MenuItem value="">{t('shareGroups.fields.templateInherit')}</MenuItem>
                {templateFiles.surge.map((item) => (
                  <MenuItem key={item.file} value={item.file}>
                    {item.file}
                  </MenuItem>
                ))}
              </TextField>
            </Grid>
          </Grid>

          <Divider textAlign="left">
            <Typography variant="caption" color="text.secondary">
              {t('shareGroups.sections.devices')}
            </Typography>
          </Divider>

          <Grid container spacing={2}>
            <Grid size={{ xs: 12, md: 4 }}>
              <TextField
                fullWidth
                select
                label={t('shareGroups.fields.deviceMode')}
                value={form.device_mode}
                onChange={(e) => update({ device_mode: e.target.value })}
              >
                {DEVICE_MODES.map((mode) => (
                  <MenuItem key={mode} value={mode}>
                    {t(`shareGroups.deviceModes.${mode}`)}
                  </MenuItem>
                ))}
              </TextField>
            </Grid>
            <Grid size={{ xs: 12, md: 4 }}>
              <TextField
                fullWidth
                type="number"
                label={t('shareGroups.fields.maxDevices')}
                value={form.max_devices}
                onChange={(e) => update({ max_devices: e.target.value })}
                helperText={t('shareGroups.fields.unlimitedHint')}
                slotProps={{ htmlInput: { min: 0 } }}
              />
            </Grid>
            <Grid size={{ xs: 12, md: 4 }}>
              <TextField
                fullWidth
                type="number"
                label={t('shareGroups.fields.trafficLimit')}
                value={form.traffic_limit_gb}
                onChange={(e) => update({ traffic_limit_gb: e.target.value })}
                helperText={t('shareGroups.fields.trafficLimitHint')}
                slotProps={{ htmlInput: { min: 0, step: 0.5 } }}
              />
            </Grid>
          </Grid>

          <Divider textAlign="left">
            <Typography variant="caption" color="text.secondary">
              {t('shareGroups.sections.expiry')}
            </Typography>
          </Divider>

          <Grid container spacing={2}>
            <Grid size={{ xs: 12, md: 4 }}>
              <TextField
                fullWidth
                select
                label={t('shareGroups.fields.expireType')}
                value={form.expire_type}
                onChange={(e) => update({ expire_type: Number(e.target.value) })}
              >
                <MenuItem value={0}>{t('shareGroups.fields.expireNever')}</MenuItem>
                <MenuItem value={1}>{t('shareGroups.fields.expireDays')}</MenuItem>
                <MenuItem value={2}>{t('shareGroups.fields.expireAt')}</MenuItem>
              </TextField>
            </Grid>
            {form.expire_type === 1 && (
              <Grid size={{ xs: 12, md: 4 }}>
                <TextField
                  fullWidth
                  type="number"
                  label={t('shareGroups.fields.expireDaysCount')}
                  value={form.expire_days}
                  onChange={(e) => update({ expire_days: e.target.value })}
                  slotProps={{ htmlInput: { min: 1 } }}
                />
              </Grid>
            )}
            {form.expire_type === 2 && (
              <Grid size={{ xs: 12, md: 6 }}>
                <TextField
                  fullWidth
                  type="datetime-local"
                  label={t('shareGroups.fields.expireAt')}
                  value={form.expire_at}
                  onChange={(e) => update({ expire_at: e.target.value })}
                  slotProps={{ inputLabel: { shrink: true } }}
                />
              </Grid>
            )}
          </Grid>

          <Divider textAlign="left">
            <Typography variant="caption" color="text.secondary">
              {t('shareGroups.sections.telemetry')}
            </Typography>
          </Divider>

          <FormControlLabel
            control={<Switch checked={form.telemetry_enabled} onChange={(e) => update({ telemetry_enabled: e.target.checked })} />}
            label={t('shareGroups.fields.telemetryEnabled')}
          />
          <Grid container spacing={2}>
            {['sessions', 'nodes', 'reliability'].map((channel) => (
              <Grid key={channel} size={{ xs: 12, md: 4 }}>
                <TextField
                  fullWidth
                  type="number"
                  disabled={!form.telemetry_enabled}
                  label={t(`shareGroups.fields.interval_${channel}`)}
                  value={form[`report_interval_${channel}`]}
                  onChange={(e) => update({ [`report_interval_${channel}`]: e.target.value })}
                  helperText={t('shareGroups.fields.intervalHint')}
                  slotProps={{ htmlInput: { min: 300, max: 86400, step: 300 } }}
                />
              </Grid>
            ))}
          </Grid>
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>{t('common.cancel')}</Button>
        <Button variant="contained" onClick={handleSave} disabled={saving}>
          {t('common.save')}
        </Button>
      </DialogActions>
    </Dialog>
  );
}

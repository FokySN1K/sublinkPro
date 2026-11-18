import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';

import Alert from '@mui/material/Alert';
import Box from '@mui/material/Box';
import Chip from '@mui/material/Chip';
import CircularProgress from '@mui/material/CircularProgress';
import Dialog from '@mui/material/Dialog';
import DialogActions from '@mui/material/DialogActions';
import DialogContent from '@mui/material/DialogContent';
import DialogTitle from '@mui/material/DialogTitle';
import Button from '@mui/material/Button';
import IconButton from '@mui/material/IconButton';
import Stack from '@mui/material/Stack';
import Table from '@mui/material/Table';
import TableBody from '@mui/material/TableBody';
import TableCell from '@mui/material/TableCell';
import TableContainer from '@mui/material/TableContainer';
import TableHead from '@mui/material/TableHead';
import TableRow from '@mui/material/TableRow';
import Tooltip from '@mui/material/Tooltip';
import Typography from '@mui/material/Typography';
import useMediaQuery from '@mui/material/useMediaQuery';
import { useTheme } from '@mui/material/styles';

import DeleteIcon from '@mui/icons-material/Delete';
import BlockIcon from '@mui/icons-material/Block';
import CheckCircleIcon from '@mui/icons-material/CheckCircle';
import LockOpenIcon from '@mui/icons-material/LockOpen';

import { formatDateTime } from 'i18n/locales';
import { formatBytes } from 'views/airports/utils';
import { getShareDevices, updateShareDevice, deleteShareDevice } from 'api/shareGroups';

function statusColor(status) {
  switch (status) {
    case 'active':
      return 'success';
    case 'pending':
      return 'warning';
    case 'blocked':
      return 'error';
    default:
      return 'default';
  }
}

export default function ShareDevicesDialog({ open, share, onClose, showMessage }) {
  const { t, i18n } = useTranslation();
  const theme = useTheme();
  const fullScreen = useMediaQuery(theme.breakpoints.down('sm'));
  const language = i18n.resolvedLanguage || i18n.language;

  const [loading, setLoading] = useState(false);
  const [items, setItems] = useState([]);
  const [meta, setMeta] = useState({ active: 0, pending: 0, blocked: 0, max_devices: 0 });

  const fetchDevices = useCallback(async () => {
    if (!share?.id) {
      setItems([]);
      return;
    }
    setLoading(true);
    try {
      const res = await getShareDevices(share.id);
      setItems(res.data?.items || []);
      setMeta({
        active: res.data?.active || 0,
        pending: res.data?.pending || 0,
        blocked: res.data?.blocked || 0,
        max_devices: res.data?.max_devices || 0
      });
    } catch (error) {
      showMessage?.(error.response?.data?.msg || t('shareGroups.telemetry.loadFailed'), 'error');
    } finally {
      setLoading(false);
    }
  }, [share?.id, showMessage, t]);

  useEffect(() => {
    if (!open) return;
    fetchDevices();
  }, [open, fetchDevices]);

  const handleStatus = useCallback(
    async (device, status) => {
      try {
        await updateShareDevice({ id: device.id, status });
        showMessage?.(t('subscriptions.share.devicesDialog.updated'));
        fetchDevices();
      } catch (error) {
        showMessage?.(error.response?.data?.msg || t('subscriptions.share.devicesDialog.updateFailed'), 'error');
      }
    },
    [fetchDevices, showMessage, t]
  );

  const handleDelete = useCallback(
    async (device) => {
      const name = device.name || device.device_id || `#${device.id}`;
      if (!window.confirm(t('subscriptions.share.devicesDialog.confirmDelete', { name }))) return;
      try {
        await deleteShareDevice(device.id);
        showMessage?.(t('subscriptions.share.devicesDialog.deleted'));
        fetchDevices();
      } catch (error) {
        showMessage?.(error.response?.data?.msg || t('subscriptions.share.devicesDialog.deleteFailed'), 'error');
      }
    },
    [fetchDevices, showMessage, t]
  );

  const limitLabel = useMemo(() => {
    if (meta.max_devices > 0) {
      return t('subscriptions.share.devicesDialog.limit', { active: meta.active, limit: meta.max_devices });
    }
    return t('subscriptions.share.devicesDialog.unlimited');
  }, [meta, t]);

  const deviceName = (device) => device.name || device.device_id || `#${device.id}`;

  return (
    <Dialog open={open} onClose={onClose} maxWidth="md" fullWidth fullScreen={fullScreen}>
      <DialogTitle>{t('subscriptions.share.devicesTitle', { name: share?.name || '' })}</DialogTitle>
      <DialogContent dividers>
        <Stack direction="row" spacing={0.75} flexWrap="wrap" useFlexGap sx={{ mb: 1.5 }}>
          <Chip size="small" color="success" variant="outlined" label={`${t('shareGroups.deviceStatus.active')}: ${meta.active}`} />
          <Chip size="small" color="warning" variant="outlined" label={`${t('shareGroups.deviceStatus.pending')}: ${meta.pending}`} />
          <Chip size="small" color="error" variant="outlined" label={`${t('shareGroups.deviceStatus.blocked')}: ${meta.blocked}`} />
          <Chip size="small" variant="outlined" label={limitLabel} />
        </Stack>

        {loading ? (
          <Stack alignItems="center" sx={{ py: 6 }}>
            <CircularProgress />
          </Stack>
        ) : items.length === 0 ? (
          <Alert severity="info">{t('subscriptions.share.devicesDialog.empty')}</Alert>
        ) : (
          <TableContainer>
            <Table size="small">
              <TableHead>
                <TableRow>
                  <TableCell>{t('subscriptions.share.devicesDialog.columns.device')}</TableCell>
                  <TableCell>{t('subscriptions.share.devicesDialog.columns.status')}</TableCell>
                  <TableCell>{t('subscriptions.share.devicesDialog.columns.traffic')}</TableCell>
                  <TableCell>{t('subscriptions.share.devicesDialog.columns.lastSeen')}</TableCell>
                  <TableCell align="right" />
                </TableRow>
              </TableHead>
              <TableBody>
                {items.map((device) => (
                  <TableRow key={device.id} hover>
                    <TableCell>
                      <Typography variant="body2" fontWeight={600} noWrap>
                        {deviceName(device)}
                      </Typography>
                      <Typography variant="caption" color="text.secondary" display="block" noWrap>
                        {[device.platform, device.app_version].filter(Boolean).join(' · ') || device.device_id}
                      </Typography>
                      <Typography variant="caption" color="text.secondary" display="block" noWrap sx={{ fontFamily: 'monospace' }}>
                        {device.hwid}
                      </Typography>
                    </TableCell>
                    <TableCell>
                      <Chip
                        size="small"
                        color={statusColor(device.status)}
                        variant="outlined"
                        label={t(`shareGroups.deviceStatus.${device.status || 'active'}`)}
                      />
                    </TableCell>
                    <TableCell>
                      <Typography variant="body2">{formatBytes(device.reported_traffic_bytes || 0)}</Typography>
                      <Typography variant="caption" color="text.secondary" display="block" noWrap>
                        {t('subscriptions.share.devicesDialog.columns.lastReport')}:{' '}
                        {device.last_report_at ? formatDateTime(device.last_report_at, language) : '-'}
                      </Typography>
                    </TableCell>
                    <TableCell>
                      <Typography variant="body2" noWrap>
                        {device.last_seen_at ? formatDateTime(device.last_seen_at, language) : '-'}
                      </Typography>
                    </TableCell>
                    <TableCell align="right">
                      <Box sx={{ display: 'flex', justifyContent: 'flex-end', gap: 0.25 }}>
                        {device.status !== 'active' && (
                          <Tooltip title={t('subscriptions.share.devicesDialog.actions.approve')}>
                            <IconButton size="small" color="success" onClick={() => handleStatus(device, 'active')}>
                              <CheckCircleIcon fontSize="small" />
                            </IconButton>
                          </Tooltip>
                        )}
                        {device.status !== 'blocked' ? (
                          <Tooltip title={t('subscriptions.share.devicesDialog.actions.block')}>
                            <IconButton size="small" color="warning" onClick={() => handleStatus(device, 'blocked')}>
                              <BlockIcon fontSize="small" />
                            </IconButton>
                          </Tooltip>
                        ) : (
                          <Tooltip title={t('subscriptions.share.devicesDialog.actions.unblock')}>
                            <IconButton size="small" onClick={() => handleStatus(device, 'active')}>
                              <LockOpenIcon fontSize="small" />
                            </IconButton>
                          </Tooltip>
                        )}
                        <Tooltip title={t('subscriptions.share.devicesDialog.actions.delete')}>
                          <IconButton size="small" color="error" onClick={() => handleDelete(device)}>
                            <DeleteIcon fontSize="small" />
                          </IconButton>
                        </Tooltip>
                      </Box>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </TableContainer>
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>{t('common.close')}</Button>
      </DialogActions>
    </Dialog>
  );
}

import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import Dialog from '@mui/material/Dialog';
import DialogTitle from '@mui/material/DialogTitle';
import DialogContent from '@mui/material/DialogContent';
import DialogActions from '@mui/material/DialogActions';
import Button from '@mui/material/Button';
import Stack from '@mui/material/Stack';
import Table from '@mui/material/Table';
import TableBody from '@mui/material/TableBody';
import TableCell from '@mui/material/TableCell';
import TableHead from '@mui/material/TableHead';
import TableRow from '@mui/material/TableRow';
import Chip from '@mui/material/Chip';
import Typography from '@mui/material/Typography';
import CircularProgress from '@mui/material/CircularProgress';

import { getShareGroupActivity } from 'api/shareGroups';
import { formatDateTime } from 'i18n/locales';

// Mirrors models.ShareAccessAction* on the backend.
const ACTION_COLORS = {
  fetch: 'success',
  status: 'info',
  stub: 'warning',
  bind: 'secondary',
  admin: 'default',
  telemetry: 'primary'
};

// Mirrors models.ShareStubCaseKeys — reasons reuse the stub case keys.
const KNOWN_REASONS = [
  'device_limit',
  'device_pending',
  'device_blocked',
  'quota_exhausted',
  'expired',
  'group_disabled',
  'unsupported_app',
  'template_disabled',
  'invalid_token'
];

export default function ShareGroupActivityDialog({ open, group, onClose }) {
  const { t, i18n } = useTranslation();
  const [rows, setRows] = useState([]);
  const [loading, setLoading] = useState(false);

  const actionLabel = (action) => (ACTION_COLORS[action] ? t(`shareGroups.activityDialog.actions.${action}`) : action || '-');
  const reasonLabel = (reason) => (KNOWN_REASONS.includes(reason) ? t(`shareGroups.activityDialog.reasons.${reason}`) : reason || '-');

  useEffect(() => {
    if (!open || !group?.id) return;
    let active = true;
    setLoading(true);
    getShareGroupActivity(group.id)
      .then((res) => {
        if (active) setRows(res.data || []);
      })
      .catch(() => {
        if (active) setRows([]);
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [open, group?.id]);

  return (
    <Dialog open={open} onClose={onClose} maxWidth="md" fullWidth>
      <DialogTitle>{t('shareGroups.activityDialog.title', { name: group?.name })}</DialogTitle>
      <DialogContent dividers>
        {loading ? (
          <Stack alignItems="center" sx={{ py: 4 }}>
            <CircularProgress size={28} />
          </Stack>
        ) : rows.length === 0 ? (
          <Typography variant="body2" color="text.secondary">
            {t('shareGroups.activityDialog.empty')}
          </Typography>
        ) : (
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>{t('shareGroups.activityDialog.time')}</TableCell>
                <TableCell>{t('shareGroups.activityDialog.share')}</TableCell>
                <TableCell>{t('shareGroups.activityDialog.device')}</TableCell>
                <TableCell>{t('shareGroups.activityDialog.action')}</TableCell>
                <TableCell>{t('shareGroups.activityDialog.template')}</TableCell>
                <TableCell>{t('shareGroups.activityDialog.reason')}</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {rows.map((row) => (
                <TableRow key={row.id}>
                  <TableCell>{formatDateTime(row.created_at, i18n.resolvedLanguage || i18n.language)}</TableCell>
                  <TableCell>{row.share_name || `#${row.share_id}`}</TableCell>
                  <TableCell>
                    <Typography variant="body2">{row.device_id || '-'}</Typography>
                    <Typography variant="caption" color="text.secondary">
                      {row.hwid || ''}
                    </Typography>
                  </TableCell>
                  <TableCell>
                    <Chip size="small" variant="outlined" color={ACTION_COLORS[row.action] || 'default'} label={actionLabel(row.action)} />
                  </TableCell>
                  <TableCell>{row.template || '-'}</TableCell>
                  <TableCell>{reasonLabel(row.reason)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>{t('common.close')}</Button>
      </DialogActions>
    </Dialog>
  );
}

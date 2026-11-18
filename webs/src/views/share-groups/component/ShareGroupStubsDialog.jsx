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
import MenuItem from '@mui/material/MenuItem';
import Select from '@mui/material/Select';
import Typography from '@mui/material/Typography';
import CircularProgress from '@mui/material/CircularProgress';

import { getShareGroupStubs, updateShareGroupStubs } from 'api/shareGroups';

export default function ShareGroupStubsDialog({ open, group, stubs, cases, onClose, onSaved, showMessage }) {
  const { t } = useTranslation();
  const [mapping, setMapping] = useState({});
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!open || !group?.id) return;
    let active = true;
    setLoading(true);
    getShareGroupStubs(group.id)
      .then((res) => {
        if (active) setMapping(res.data || {});
      })
      .catch(() => {
        if (active) setMapping({});
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [open, group?.id]);

  const handleSave = async () => {
    const payload = {};
    Object.entries(mapping).forEach(([caseKey, stubId]) => {
      if (stubId > 0) payload[caseKey] = Number(stubId);
    });
    setSaving(true);
    try {
      await updateShareGroupStubs(group.id, payload);
      showMessage?.(t('shareGroups.messages.stubsSaved'), 'success');
      onSaved?.();
      onClose?.();
    } catch (error) {
      showMessage?.(error.response?.data?.msg || t('shareGroups.messages.saveFailed'), 'error');
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle>{t('shareGroups.stubsDialog.title', { name: group?.name })}</DialogTitle>
      <DialogContent dividers>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          {t('shareGroups.stubsDialog.hint')}
        </Typography>
        {loading ? (
          <Stack alignItems="center" sx={{ py: 4 }}>
            <CircularProgress size={28} />
          </Stack>
        ) : (
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>{t('shareGroups.stubsDialog.case')}</TableCell>
                <TableCell width="55%">{t('shareGroups.stubsDialog.stub')}</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {cases.map((item) => (
                <TableRow key={item.key}>
                  <TableCell>
                    <Typography variant="body2">{item.label}</Typography>
                    <Typography variant="caption" color="text.secondary">
                      {item.key}
                    </Typography>
                  </TableCell>
                  <TableCell>
                    <Select
                      size="small"
                      fullWidth
                      displayEmpty
                      value={mapping[item.key] || 0}
                      onChange={(e) => setMapping((prev) => ({ ...prev, [item.key]: Number(e.target.value) }))}
                    >
                      <MenuItem value={0}>{t('shareGroups.stubsDialog.useDefault')}</MenuItem>
                      {stubs
                        .filter((stub) => stub.case_key === item.key)
                        .map((stub) => (
                          <MenuItem key={stub.id} value={stub.id}>
                            {stub.name || stub.title}
                            {stub.enabled ? '' : ` (${t('common.disabled')})`}
                          </MenuItem>
                        ))}
                    </Select>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>{t('common.cancel')}</Button>
        <Button variant="contained" onClick={handleSave} disabled={saving || loading}>
          {t('common.save')}
        </Button>
      </DialogActions>
    </Dialog>
  );
}

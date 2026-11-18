import { useEffect, useState } from 'react';
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
import Typography from '@mui/material/Typography';

import { createShareStub, updateShareStub } from 'api/shareGroups';

const EMPTY = {
  name: '',
  case_key: '',
  title: '',
  content: '',
  enabled: true
};

export default function ShareStubDialog({ open, stub, cases, onClose, onSaved, showMessage }) {
  const { t } = useTranslation();
  const [form, setForm] = useState(EMPTY);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!open) return;
    if (stub) {
      setForm({
        name: stub.name || '',
        case_key: stub.case_key || '',
        title: stub.title || '',
        content: stub.content || '',
        enabled: stub.enabled !== false
      });
    } else {
      setForm({ ...EMPTY, case_key: cases[0]?.key || '' });
    }
  }, [open, stub, cases]);

  const update = (patch) => setForm((prev) => ({ ...prev, ...patch }));

  const handleSave = async () => {
    if (!form.case_key) {
      showMessage?.(t('shareGroups.stubsDialog.caseRequired'), 'warning');
      return;
    }
    setSaving(true);
    try {
      if (stub?.id) {
        await updateShareStub({ id: stub.id, ...form });
        showMessage?.(t('shareGroups.messages.stubUpdated'), 'success');
      } else {
        await createShareStub(form);
        showMessage?.(t('shareGroups.messages.stubCreated'), 'success');
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
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle>{stub?.id ? t('shareGroups.stubDialog.editTitle') : t('shareGroups.stubDialog.createTitle')}</DialogTitle>
      <DialogContent dividers>
        <Stack spacing={2} sx={{ pt: 1 }}>
          <TextField
            select
            fullWidth
            label={t('shareGroups.stubDialog.case')}
            value={form.case_key}
            onChange={(e) => update({ case_key: e.target.value })}
          >
            {cases.map((item) => (
              <MenuItem key={item.key} value={item.key}>
                {item.label} ({item.key})
              </MenuItem>
            ))}
          </TextField>
          <TextField
            fullWidth
            label={t('shareGroups.stubDialog.name')}
            value={form.name}
            onChange={(e) => update({ name: e.target.value })}
          />
          <TextField
            fullWidth
            label={t('shareGroups.stubDialog.title')}
            value={form.title}
            onChange={(e) => update({ title: e.target.value })}
          />
          <TextField
            fullWidth
            multiline
            minRows={4}
            label={t('shareGroups.stubDialog.content')}
            value={form.content}
            onChange={(e) => update({ content: e.target.value })}
            helperText={t('shareGroups.stubDialog.placeholderHint')}
          />
          <Typography variant="caption" color="text.secondary">
            {t('shareGroups.stubDialog.placeholders')}
          </Typography>
          <FormControlLabel
            control={<Switch checked={form.enabled} onChange={(e) => update({ enabled: e.target.checked })} />}
            label={t('shareGroups.fields.enabled')}
          />
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

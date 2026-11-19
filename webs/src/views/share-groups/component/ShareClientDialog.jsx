import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';

import Button from '@mui/material/Button';
import Dialog from '@mui/material/Dialog';
import DialogActions from '@mui/material/DialogActions';
import DialogContent from '@mui/material/DialogContent';
import DialogTitle from '@mui/material/DialogTitle';
import FormControlLabel from '@mui/material/FormControlLabel';
import Stack from '@mui/material/Stack';
import Switch from '@mui/material/Switch';
import TextField from '@mui/material/TextField';

import { createShareClient, updateShareClient } from 'api/shareGroups';

const EMPTY = { name: '', note: '', enabled: true };

// Create or rename a share client (user). Deleting a client keeps its cards.
export default function ShareClientDialog({ open, client, onClose, onSaved, showMessage }) {
  const { t } = useTranslation();
  const [form, setForm] = useState(EMPTY);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!open) return;
    if (client) {
      setForm({ name: client.name || '', note: client.note || '', enabled: client.enabled !== false });
    } else {
      setForm(EMPTY);
    }
  }, [open, client]);

  const handleChange = (field) => (event) => {
    setForm((prev) => ({ ...prev, [field]: event.target.value }));
  };

  const handleSave = async () => {
    const name = form.name.trim();
    if (!name) {
      showMessage?.(t('shareGroups.clientDialog.nameRequired'), 'error');
      return;
    }
    setSaving(true);
    try {
      if (client) {
        await updateShareClient({ id: client.id, name, note: form.note.trim(), enabled: form.enabled });
        showMessage?.(t('shareGroups.clientDialog.updateSuccess'));
      } else {
        await createShareClient({ name, note: form.note.trim(), enabled: form.enabled });
        showMessage?.(t('shareGroups.clientDialog.createSuccess'));
      }
      onSaved?.();
      onClose?.();
    } catch (error) {
      showMessage?.(error.response?.data?.msg || t('shareGroups.clientDialog.saveFailed'), 'error');
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onClose={onClose} maxWidth="xs" fullWidth>
      <DialogTitle>{client ? t('shareGroups.clientDialog.editTitle') : t('shareGroups.clientDialog.createTitle')}</DialogTitle>
      <DialogContent dividers>
        <Stack spacing={2} sx={{ mt: 0.5 }}>
          <TextField
            size="small"
            required
            label={t('shareGroups.clientDialog.name')}
            value={form.name}
            onChange={handleChange('name')}
            autoFocus
          />
          <TextField
            size="small"
            label={t('shareGroups.clientDialog.note')}
            value={form.note}
            onChange={handleChange('note')}
            multiline
            minRows={2}
          />
          <FormControlLabel
            control={
              <Switch checked={form.enabled} onChange={(event) => setForm((prev) => ({ ...prev, enabled: event.target.checked }))} />
            }
            label={t('shareGroups.clientDialog.enabled')}
          />
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>{t('common.cancel')}</Button>
        <Button variant="contained" onClick={handleSave} disabled={saving}>
          {t('common.confirm')}
        </Button>
      </DialogActions>
    </Dialog>
  );
}

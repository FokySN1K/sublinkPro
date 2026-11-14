import { useTranslation } from 'react-i18next';
import useMediaQuery from '@mui/material/useMediaQuery';
import { useTheme } from '@mui/material/styles';
import Alert from '@mui/material/Alert';
import Autocomplete from '@mui/material/Autocomplete';
import Button from '@mui/material/Button';
import Dialog from '@mui/material/Dialog';
import DialogActions from '@mui/material/DialogActions';
import DialogContent from '@mui/material/DialogContent';
import DialogTitle from '@mui/material/DialogTitle';
import Stack from '@mui/material/Stack';
import TextField from '@mui/material/TextField';

/**
 * 代理订阅表单：选择要原样转发的机场，由 SublinkPro 把该机场的上游订阅内容直接转给客户端。
 */
export default function ProxySubscriptionFormDialog({ open, isEdit, formData, setFormData, airportOptions = [], onClose, onSubmit }) {
  const { t } = useTranslation();
  const theme = useTheme();
  const matchDownMd = useMediaQuery(theme.breakpoints.down('md'));

  const getAirportId = (airport) => Number(airport?.id ?? airport?.ID);
  const selectedAirport = (airportOptions || []).find((airport) => getAirportId(airport) === Number(formData.AirportID)) || null;
  // 机场可能已被删除，编辑时提示重新选择，避免保存一个失效引用。
  const airportMissing = Boolean(formData.AirportID) && !selectedAirport;

  const updateField = (field) => (event) => {
    setFormData({ ...formData, [field]: event.target.value });
  };

  const nameInvalid = !formData.name?.trim();
  const airportInvalid = !formData.AirportID;
  const confirmDisabled = nameInvalid || airportInvalid;

  return (
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle>{isEdit ? t('subscriptions.proxyDialog.editTitle') : t('subscriptions.proxyDialog.addTitle')}</DialogTitle>
      <DialogContent dividers>
        <Stack spacing={2}>
          <Alert severity="info" variant="outlined">
            {t('subscriptions.proxyDialog.description')}
          </Alert>

          <TextField
            label={t('subscriptions.proxyDialog.fields.name')}
            value={formData.name || ''}
            onChange={updateField('name')}
            required
            fullWidth
            size="small"
            error={nameInvalid && Boolean(formData.name)}
          />

          <TextField
            label={t('subscriptions.proxyDialog.fields.description')}
            value={formData.description || ''}
            onChange={updateField('description')}
            fullWidth
            size="small"
            multiline
            minRows={1}
            maxRows={3}
            slotProps={{ htmlInput: { maxLength: 512 } }}
          />

          <Autocomplete
            options={airportOptions || []}
            value={selectedAirport}
            onChange={(event, newValue) => {
              void event;
              setFormData({ ...formData, AirportID: newValue ? getAirportId(newValue) : '' });
            }}
            getOptionLabel={(option) => option?.name || option?.Name || ''}
            isOptionEqualToValue={(option, value) => getAirportId(option) === getAirportId(value)}
            size="small"
            renderInput={(params) => (
              <TextField
                {...params}
                label={t('subscriptions.proxyDialog.fields.airport')}
                required
                error={airportMissing}
                helperText={
                  airportMissing ? t('subscriptions.proxyDialog.fields.airportMissing') : t('subscriptions.proxyDialog.fields.airportHelp')
                }
              />
            )}
            renderOption={(props, option) => <li {...props}>{option.name || option.Name}</li>}
          />
        </Stack>
      </DialogContent>
      <DialogActions sx={{ px: matchDownMd ? 2 : 3, py: 1.5 }}>
        <Button onClick={onClose}>{t('common.cancel')}</Button>
        <Button variant="contained" onClick={onSubmit} disabled={confirmDisabled}>
          {t('subscriptions.proxyDialog.confirm')}
        </Button>
      </DialogActions>
    </Dialog>
  );
}

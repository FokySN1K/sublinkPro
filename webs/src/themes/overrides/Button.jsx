// ==============================|| OVERRIDES - BUTTON ||============================== //

// 按下时不再播放水波纹，改用轻微的颜色变化作为点击反馈
const pressedSx = {
  '&:active:not(.Mui-disabled)': {
    filter: 'brightness(0.94)'
  }
};

export default function Button() {
  return {
    MuiButtonBase: {
      defaultProps: {
        disableRipple: true
      }
    },
    MuiButton: {
      styleOverrides: {
        root: pressedSx
      }
    },
    MuiIconButton: {
      styleOverrides: {
        root: pressedSx
      }
    },
    MuiFab: {
      styleOverrides: {
        root: pressedSx
      }
    },
    MuiToggleButton: {
      styleOverrides: {
        root: pressedSx
      }
    }
  };
}

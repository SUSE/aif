const webpack = require('webpack');
const base = require('./.shell/pkg/vue.config')(__dirname);

const origConfigureWebpack = base.configureWebpack;

base.configureWebpack = (config) => {
  origConfigureWebpack(config);

  config.plugins.push(new webpack.DefinePlugin({
    'process.env.USE_MOCK_API': JSON.stringify(process.env.USE_MOCK_API || 'false')
  }));
};

module.exports = base;
